// app.js — Translation Combinator (modern ES module)
// API base: set via window.API_BASE (e.g. in index.html or config.local.js)

const API = (window.API_BASE || '').replace(/\/+$/, '');

// ── DOM refs ──────────────────────────────────────────────────────────────────
const $ = id => document.getElementById(id);

const uploadSection = $('upload-section');
const jobSection    = $('job-section');

const audioInput  = $('audio-file');
const vttInput    = $('vtt-file');
const audioDrop   = $('audio-drop');
const vttDrop     = $('vtt-drop');
const audioName   = $('audio-name');
const vttName     = $('vtt-name');
const audioBar    = $('audio-progress');
const vttBar      = $('vtt-progress');

const ttsProvider = $('tts-provider');
const ttsVolume   = $('tts-volume');
const volDisplay  = $('vol-display');
const noSpeedup   = $('no-speedup');
const submitBtn   = $('submit-btn');

const statusLed   = $('status-led');
const jobIdEl     = $('job-id-display');
const jobProgress = $('job-progress');
const progressOverlay = $('progress-overlay');
const waveformBars    = $('waveform-bars');

const downloadBtn = $('download-btn');
const newJobBtn   = $('new-job-btn');
const refreshBtn  = $('refresh-btn');
const jobList     = $('job-list');

// ── State ─────────────────────────────────────────────────────────────────────
let pollingTimer  = null;
let currentJobId  = null;

// ── Init ──────────────────────────────────────────────────────────────────────
initDropZone(audioDrop, audioInput, audioName);
initDropZone(vttDrop,   vttInput,   vttName);
buildWaveform();

ttsVolume.addEventListener('input', () => {
  volDisplay.textContent = parseFloat(ttsVolume.value).toFixed(2);
});

submitBtn.addEventListener('click', handleSubmit);
newJobBtn.addEventListener('click', resetToUpload);
refreshBtn.addEventListener('click', loadRecentJobs);

loadRecentJobs();

// ── Drop Zone ─────────────────────────────────────────────────────────────────
function initDropZone(zone, input, nameEl) {
  zone.addEventListener('click', () => input.click());

  input.addEventListener('change', () => {
    if (input.files[0]) selectFile(zone, nameEl, input.files[0].name);
  });

  zone.addEventListener('dragover', e => {
    e.preventDefault();
    zone.classList.add('drag-over');
  });

  zone.addEventListener('dragleave', e => {
    if (!zone.contains(e.relatedTarget)) zone.classList.remove('drag-over');
  });

  zone.addEventListener('drop', e => {
    e.preventDefault();
    zone.classList.remove('drag-over');
    const file = e.dataTransfer.files[0];
    if (!file) return;
    const dt = new DataTransfer();
    dt.items.add(file);
    input.files = dt.files;
    selectFile(zone, nameEl, file.name);
  });
}

function selectFile(zone, nameEl, filename) {
  nameEl.textContent = filename;
  zone.classList.add('has-file');
  updateSubmitState();
}

function updateSubmitState() {
  submitBtn.disabled = !(audioInput.files[0] && vttInput.files[0]);
}

// ── Waveform ──────────────────────────────────────────────────────────────────
function buildWaveform() {
  const count = 52;
  for (let i = 0; i < count; i++) {
    const bar = document.createElement('div');
    bar.className = 'bar';
    const pct = 22 + Math.abs(Math.sin(i * 0.38 + 0.5)) * 50 + Math.random() * 18;
    bar.style.height = `${Math.round(pct)}%`;
    bar.style.setProperty('--dur',   `${(0.75 + Math.random() * 0.9).toFixed(2)}s`);
    bar.style.setProperty('--delay', `${(i * 0.038).toFixed(2)}s`);
    waveformBars.appendChild(bar);
  }
}

function paintWaveform(pct) {
  const bars = waveformBars.querySelectorAll('.bar');
  const filled = Math.round((pct / 100) * bars.length);
  bars.forEach((b, i) => {
    if (i < filled) {
      b.style.background = `linear-gradient(to top, var(--teal), var(--violet))`;
      b.style.opacity = '0.75';
    } else {
      b.style.background = '';
      b.style.opacity = '';
    }
  });
  progressOverlay.style.width = `${pct}%`;
}

// ── Submit ────────────────────────────────────────────────────────────────────
async function handleSubmit() {
  if (!audioInput.files[0] || !vttInput.files[0]) return;

  submitBtn.disabled = true;
  submitBtn.querySelector('.btn-text').textContent = '上传中…';

  try {
    const [audio, vtt] = await Promise.all([
      uploadFile(audioInput.files[0], audioBar),
      uploadFile(vttInput.files[0],   vttBar),
    ]);

    submitBtn.querySelector('.btn-text').textContent = '创建任务…';
    const job = await createJob(audio, vtt);

    showJobSection(job);
    startPolling(job.job_id);
  } catch (err) {
    console.error(err);
    alert('操作失败: ' + err.message);
    submitBtn.disabled = false;
    submitBtn.querySelector('.btn-text').textContent = '开始处理';
  }
}

// Returns { key, name } for the uploaded file.
async function uploadFile(file, progressEl) {
  const res = await fetch(`${API}/api/upload-url`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      filename: file.name,
      content_type: file.type || 'application/octet-stream',
    }),
  });
  if (!res.ok) throw new Error('获取上传链接失败');
  const { upload_url, object_key } = await res.json();

  await xhrUpload(file, upload_url, pct => { progressEl.style.width = `${pct}%`; });
  return { key: object_key, name: file.name };
}

function xhrUpload(file, url, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.upload.addEventListener('progress', e => {
      if (e.lengthComputable) onProgress(Math.round((e.loaded / e.total) * 100));
    });
    xhr.addEventListener('load', () => {
      onProgress(100);
      xhr.status >= 200 && xhr.status < 300 ? resolve() : reject(new Error(`上传失败: ${xhr.status}`));
    });
    xhr.addEventListener('error', () => reject(new Error('网络错误')));
    xhr.open('PUT', url);
    xhr.setRequestHeader('Content-Type', file.type || 'application/octet-stream');
    xhr.send(file);
  });
}

async function createJob(audio, vtt) {
  const res = await fetch(`${API}/api/jobs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({
      audio_key:    audio.key,
      vtt_key:      vtt.key,
      audio_name:   audio.name,
      vtt_name:     vtt.name,
      tts_provider: ttsProvider.value,
      tts_volume:   parseFloat(ttsVolume.value),
      no_speedup:   noSpeedup.checked,
      concurrency:  5,
    }),
  });
  if (!res.ok) {
    const data = await res.json().catch(() => ({}));
    throw new Error(data.error || '创建任务失败');
  }
  return res.json();
}

// ── Job Section ───────────────────────────────────────────────────────────────
function showJobSection(job) {
  uploadSection.hidden = true;
  jobSection.hidden = false;
  jobSection.classList.add('fade-in');
  currentJobId = job.job_id;
  updateJobDisplay(job);
}

function updateJobDisplay(job) {
  const { status, job_id, audio_name, progress, error } = job;

  statusLed.className = `status-indicator ${status}`;
  // Show original filename if available, fall back to job ID
  jobIdEl.textContent = audio_name ? audio_name : `Job · ${job_id}`;

  if (status === 'failed') {
    jobProgress.textContent = error || '处理失败';
    jobProgress.style.color = 'var(--s-fail)';
    newJobBtn.hidden = false;
    paintWaveform(0);
    return;
  }

  jobProgress.style.color = '';
  jobProgress.textContent = progress || statusLabel(status);

  const pct = computeProgress(status, progress);
  paintWaveform(pct);

  if (status === 'completed') {
    downloadBtn.hidden = false;
    newJobBtn.hidden = false;
    downloadBtn.onclick = () => { window.location.href = `${API}/api/jobs/${job_id}/download`; };
  }
}

function statusLabel(s) {
  return { queued: '等待队列…', processing: '正在合成音频…', completed: '处理完成', failed: '处理失败' }[s] || s;
}

function computeProgress(status, progress) {
  if (status === 'completed') return 100;
  if (status === 'queued')    return 5;
  if (progress) {
    const m = progress.match(/(\d+)\/(\d+)/);
    if (m) return Math.round((parseInt(m[1]) / parseInt(m[2])) * 100);
  }
  return 35;
}

// ── Polling ───────────────────────────────────────────────────────────────────
function startPolling(jobId) {
  stopPolling();
  pollingTimer = setInterval(async () => {
    try {
      const res = await fetch(`${API}/api/jobs/${jobId}`);
      if (!res.ok) return;
      const job = await res.json();
      updateJobDisplay(job);
      if (job.status === 'completed' || job.status === 'failed') {
        stopPolling();
        loadRecentJobs();
      }
    } catch { /* ignore transient polling errors */ }
  }, 3000);
}

function stopPolling() {
  if (pollingTimer) { clearInterval(pollingTimer); pollingTimer = null; }
}

// ── Recent Jobs ───────────────────────────────────────────────────────────────
async function loadRecentJobs() {
  try {
    const res = await fetch(`${API}/api/jobs`);
    if (!res.ok) return;
    renderJobList(await res.json());
  } catch { /* ignore */ }
}

function renderJobList(jobs) {
  if (!jobs?.length) {
    jobList.innerHTML = '<div class="job-list-empty">暂无任务</div>';
    return;
  }
  jobList.innerHTML = jobs.map(j => `
    <div class="job-item">
      <div class="job-dot ${j.status}"></div>
      <div class="job-item-id">${j.audio_name || j.job_id}</div>
      <div class="job-badge ${j.status}">${j.status}</div>
      <div class="job-time">${fmtTime(j.created_at)}</div>
      ${j.status === 'completed'
        ? `<a class="job-download-link" href="${API}/api/jobs/${j.job_id}/download">下载</a>`
        : ''}
    </div>
  `).join('');
}

function fmtTime(ts) {
  if (!ts) return '';
  return new Date(ts).toLocaleString('zh-CN', {
    month: 'numeric', day: 'numeric',
    hour: '2-digit', minute: '2-digit',
  });
}

// ── Reset ─────────────────────────────────────────────────────────────────────
function resetToUpload() {
  stopPolling();
  currentJobId = null;
  uploadSection.hidden = false;
  jobSection.hidden = true;
  uploadSection.classList.add('fade-in');

  audioInput.value = '';
  vttInput.value = '';
  audioDrop.classList.remove('has-file');
  vttDrop.classList.remove('has-file');
  audioName.textContent = '';
  vttName.textContent = '';
  audioBar.style.width = '0%';
  vttBar.style.width = '0%';

  downloadBtn.hidden = true;
  newJobBtn.hidden = true;
  jobProgress.style.color = '';
  submitBtn.disabled = true;
  submitBtn.querySelector('.btn-text').textContent = '开始处理';

  loadRecentJobs();
}
