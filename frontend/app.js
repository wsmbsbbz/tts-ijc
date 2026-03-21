// API base URL — set via global window.API_BASE or default to same origin.
// In production (Cloudflare Pages etc.), configure this in index.html:
//   <script>window.API_BASE = 'https://api.example.com';</script>
const API = (window.API_BASE || '').replace(/\/+$/, '');

// --- DOM refs ---
const audioInput = document.getElementById('audio-file');
const vttInput = document.getElementById('vtt-file');
const ttsProvider = document.getElementById('tts-provider');
const ttsVolume = document.getElementById('tts-volume');
const volumeValue = document.getElementById('volume-value');
const submitBtn = document.getElementById('submit-btn');
const uploadProgress = document.getElementById('upload-progress');
const uploadSection = document.getElementById('upload-section');
const jobSection = document.getElementById('job-section');
const jobStatus = document.getElementById('job-status');
const jobProgress = document.getElementById('job-progress');
const downloadLink = document.getElementById('download-link');
const newJobBtn = document.getElementById('new-job-btn');
const jobList = document.getElementById('job-list');

// --- State ---
let pollingTimer = null;

// --- Init ---
ttsVolume.addEventListener('input', () => {
  volumeValue.textContent = ttsVolume.value;
});

audioInput.addEventListener('change', updateSubmitState);
vttInput.addEventListener('change', updateSubmitState);

submitBtn.addEventListener('click', handleSubmit);
newJobBtn.addEventListener('click', resetToUpload);

loadRecentJobs();

function updateSubmitState() {
  submitBtn.disabled = !(audioInput.files.length && vttInput.files.length);
}

// --- Upload & Create Job ---
async function handleSubmit() {
  submitBtn.disabled = true;
  uploadProgress.hidden = false;
  setUploadProgress(0, '上传音频文件...');

  try {
    const audioKey = await uploadFile(audioInput.files[0], () => setUploadProgress(30, '上传音频文件...'));
    setUploadProgress(50, '上传字幕文件...');
    const vttKey = await uploadFile(vttInput.files[0], () => setUploadProgress(70, '上传字幕文件...'));
    setUploadProgress(90, '创建任务...');

    const job = await createJob(audioKey, vttKey);
    setUploadProgress(100, '任务已创建');

    showJobStatus(job);
    startPolling(job.job_id);
  } catch (err) {
    alert('操作失败: ' + err.message);
    submitBtn.disabled = false;
    uploadProgress.hidden = true;
  }
}

async function uploadFile(file) {
  const resp = await fetch(API + '/api/upload-url', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ filename: file.name, content_type: file.type || 'application/octet-stream' }),
  });

  if (!resp.ok) throw new Error('获取上传链接失败');
  const { upload_url, object_key } = await resp.json();

  const putResp = await fetch(upload_url, {
    method: 'PUT',
    headers: { 'Content-Type': file.type || 'application/octet-stream' },
    body: file,
  });

  if (!putResp.ok) throw new Error('文件上传失败');
  return object_key;
}

async function createJob(audioKey, vttKey) {
  const resp = await fetch(API + '/api/jobs', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      audio_key: audioKey,
      vtt_key: vttKey,
      tts_provider: ttsProvider.value,
      tts_volume: parseFloat(ttsVolume.value),
      no_speedup: false,
      concurrency: 5,
    }),
  });

  if (!resp.ok) {
    const data = await resp.json().catch(() => ({}));
    throw new Error(data.error || '创建任务失败');
  }
  return resp.json();
}

// --- Job Status ---
function showJobStatus(job) {
  uploadSection.hidden = true;
  jobSection.hidden = false;
  downloadLink.hidden = true;
  newJobBtn.hidden = true;
  updateJobDisplay(job);
}

function updateJobDisplay(job) {
  const badge = jobStatus.querySelector('.status-badge');
  badge.className = 'status-badge ' + job.status;

  const labels = { queued: '排队中', processing: '处理中', completed: '已完成', failed: '失败' };
  badge.textContent = labels[job.status] || job.status;

  jobProgress.textContent = job.progress || '';

  if (job.status === 'completed' && job.download_url) {
    downloadLink.href = job.download_url;
    downloadLink.hidden = false;
    downloadLink.textContent = '下载结果';
    newJobBtn.hidden = false;
  }

  if (job.status === 'failed') {
    jobProgress.textContent = '错误: ' + (job.error || '未知错误');
    newJobBtn.hidden = false;
  }
}

function startPolling(jobId) {
  stopPolling();
  pollingTimer = setInterval(async () => {
    try {
      const resp = await fetch(API + '/api/jobs/' + jobId);
      if (!resp.ok) return;
      const job = await resp.json();
      updateJobDisplay(job);

      if (job.status === 'completed' || job.status === 'failed') {
        stopPolling();
        loadRecentJobs();
      }
    } catch { /* ignore polling errors */ }
  }, 3000);
}

function stopPolling() {
  if (pollingTimer) {
    clearInterval(pollingTimer);
    pollingTimer = null;
  }
}

function resetToUpload() {
  stopPolling();
  uploadSection.hidden = false;
  jobSection.hidden = true;
  uploadProgress.hidden = true;
  audioInput.value = '';
  vttInput.value = '';
  submitBtn.disabled = true;
  loadRecentJobs();
}

// --- Recent Jobs ---
async function loadRecentJobs() {
  try {
    const resp = await fetch(API + '/api/jobs');
    if (!resp.ok) return;
    const jobs = await resp.json();
    renderJobList(jobs);
  } catch { /* ignore */ }
}

function renderJobList(jobs) {
  if (!jobs || jobs.length === 0) {
    jobList.innerHTML = '<p class="empty-state">暂无任务</p>';
    return;
  }

  jobList.innerHTML = jobs.map(j => {
    const time = new Date(j.created_at).toLocaleString('zh-CN');
    const shortId = j.job_id.slice(0, 8);
    return `
      <div class="job-item">
        <span class="job-item-id">${shortId}</span>
        <span class="status-badge ${j.status}">${j.status}</span>
        <span class="job-item-time">${time}</span>
      </div>
    `;
  }).join('');
}

// --- Helpers ---
function setUploadProgress(pct, text) {
  uploadProgress.querySelector('.progress-fill').style.width = pct + '%';
  uploadProgress.querySelector('.progress-text').textContent = text;
}
