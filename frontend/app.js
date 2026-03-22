// app.js — Translation Combinator (modern ES module)
// API base: set via window.API_BASE (e.g. in index.html or config.local.js)

const API = (window.API_BASE || '').replace(/\/+$/, '');
const TOKEN_KEY = 'tc_token';

// ── DOM refs ──────────────────────────────────────────────────────────────────
const $ = id => document.getElementById(id);

// Auth
const authSection   = $('auth-section');
const appMain       = $('app-main');
const recentSection = $('recent-section');
const userInfo      = $('user-info');
const usernameDisplay = $('username-display');
const logoutBtn     = $('logout-btn');

const tabLogin    = $('tab-login');
const tabRegister = $('tab-register');
const formLogin   = $('form-login');
const formRegister = $('form-register');

const loginUsername  = $('login-username');
const loginPassword  = $('login-password');
const loginError     = $('login-error');
const loginBtn       = $('login-btn');

const regUsername = $('reg-username');
const regPassword = $('reg-password');
const regError    = $('reg-error');
const registerBtn = $('register-btn');

// App
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
logoutBtn.addEventListener('click', handleLogout);

// Auth tab switching
tabLogin.addEventListener('click', () => switchTab('login'));
tabRegister.addEventListener('click', () => switchTab('register'));

loginBtn.addEventListener('click', handleLogin);
registerBtn.addEventListener('click', handleRegister);

// Allow Enter key in auth forms
loginPassword.addEventListener('keydown', e => { if (e.key === 'Enter') handleLogin(); });
loginUsername.addEventListener('keydown', e => { if (e.key === 'Enter') handleLogin(); });
regPassword.addEventListener('keydown', e => { if (e.key === 'Enter') handleRegister(); });

// Bootstrap
loadTTSProviders();
checkAuth();

// ── TTS Providers ─────────────────────────────────────────────────────────────
const providerLabels = {
  edge:   'Edge TTS (Microsoft)',
  gtts:   'gTTS (Google)',
  azure:  'Azure Cognitive',
  openai: 'OpenAI TTS',
  gcloud: 'Google Cloud',
};

async function loadTTSProviders() {
  try {
    const res = await fetch(`${API}/api/tts-providers`);
    if (!res.ok) return;
    const providers = await res.json();
    ttsProvider.innerHTML = providers
      .map(p => `<option value="${p}">${providerLabels[p] || p}</option>`)
      .join('');
  } catch { /* keep select empty, submit is disabled until files chosen */ }
}

// ── Auth ──────────────────────────────────────────────────────────────────────
function getToken() {
  return localStorage.getItem(TOKEN_KEY);
}

function setToken(token) {
  localStorage.setItem(TOKEN_KEY, token);
}

function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

function authHeaders() {
  const token = getToken();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function checkAuth() {
  const token = getToken();
  if (!token) {
    showAuth();
    return;
  }
  // Validate by trying a protected endpoint.
  try {
    const res = await fetch(`${API}/api/jobs`, { headers: authHeaders() });
    if (res.status === 401) {
      clearToken();
      showAuth();
    } else {
      showApp();
    }
  } catch {
    showApp(); // network error — show app optimistically, requests will fail individually
  }
}

function showAuth() {
  authSection.hidden = false;
  appMain.hidden = true;
  recentSection.hidden = true;
  userInfo.hidden = true;
}

function showApp() {
  authSection.hidden = true;
  appMain.hidden = false;
  recentSection.hidden = false;
  userInfo.hidden = false;
  loadRecentJobs();
}

function switchTab(tab) {
  const isLogin = tab === 'login';
  tabLogin.classList.toggle('active', isLogin);
  tabRegister.classList.toggle('active', !isLogin);
  formLogin.hidden = !isLogin;
  formRegister.hidden = isLogin;
  loginError.hidden = true;
  regError.hidden = true;
}

async function handleLogin() {
  const username = loginUsername.value.trim();
  const password = loginPassword.value;
  if (!username || !password) {
    showAuthError(loginError, '请输入用户名和密码');
    return;
  }

  loginBtn.disabled = true;
  loginError.hidden = true;
  try {
    const res = await fetch(`${API}/api/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    const data = await res.json();
    if (!res.ok) {
      showAuthError(loginError, data.error || '登录失败');
      return;
    }
    setToken(data.token);
    usernameDisplay.textContent = username;
    showApp();
  } catch {
    showAuthError(loginError, '网络错误，请重试');
  } finally {
    loginBtn.disabled = false;
  }
}

async function handleRegister() {
  const username = regUsername.value.trim();
  const password = regPassword.value;
  if (!username || !password) {
    showAuthError(regError, '请输入用户名和密码');
    return;
  }

  registerBtn.disabled = true;
  regError.hidden = true;
  try {
    const res = await fetch(`${API}/api/auth/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
    const data = await res.json();
    if (!res.ok) {
      showAuthError(regError, data.error || '注册失败');
      return;
    }
    setToken(data.token);
    usernameDisplay.textContent = username;
    showApp();
  } catch {
    showAuthError(regError, '网络错误，请重试');
  } finally {
    registerBtn.disabled = false;
  }
}

async function handleLogout() {
  const token = getToken();
  if (token) {
    await fetch(`${API}/api/auth/logout`, {
      method: 'POST',
      headers: { ...authHeaders() },
    }).catch(() => {});
  }
  clearToken();
  showAuth();
  switchTab('login');
  loginUsername.value = '';
  loginPassword.value = '';
}

function showAuthError(el, msg) {
  el.textContent = msg;
  el.hidden = false;
}

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
    if (err.status === 401) {
      clearToken();
      showAuth();
      return;
    }
    console.error(err);
    alert('操作失败: ' + err.message);
    submitBtn.disabled = false;
    submitBtn.querySelector('.btn-text').textContent = '开始处理';
  }
}

// Returns { key, name } for the uploaded file.
async function uploadFile(file, progressEl) {
  const res = await apiFetch(`${API}/api/upload-url`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
    body: JSON.stringify({
      filename: file.name,
      content_type: file.type || 'application/octet-stream',
    }),
  });
  if (!res.ok) {
    const e = new Error('获取上传链接失败');
    e.status = res.status;
    throw e;
  }
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
  const res = await apiFetch(`${API}/api/jobs`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...authHeaders() },
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
    const e = new Error(data.error || '创建任务失败');
    e.status = res.status;
    throw e;
  }
  return res.json();
}

// apiFetch wraps fetch and handles 401 → logout.
async function apiFetch(url, opts) {
  const res = await fetch(url, opts);
  if (res.status === 401) {
    clearToken();
    showAuth();
    const e = new Error('Session expired');
    e.status = 401;
    throw e;
  }
  return res;
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
    downloadBtn.onclick = () => { window.location.href = `${API}/api/jobs/${job_id}/download?token=${getToken()}`; };
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
      const res = await fetch(`${API}/api/jobs/${jobId}`, { headers: authHeaders() });
      if (res.status === 401) { clearToken(); showAuth(); stopPolling(); return; }
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
    const res = await fetch(`${API}/api/jobs`, { headers: authHeaders() });
    if (res.status === 401) { clearToken(); showAuth(); return; }
    if (!res.ok) return;
    renderJobList(await res.json());
  } catch { /* ignore */ }
}

function renderJobList(jobs) {
  if (!jobs?.length) {
    jobList.innerHTML = '<div class="job-list-empty">暂无任务</div>';
    return;
  }
  const token = getToken();
  jobList.innerHTML = jobs.map(j => `
    <div class="job-item">
      <div class="job-dot ${j.status}"></div>
      <div class="job-item-id">${j.audio_name || j.job_id}</div>
      <div class="job-badge ${j.status}">${j.status}</div>
      <div class="job-time">${fmtTime(j.created_at)}</div>
      ${j.status === 'completed'
        ? `<a class="job-download-link" href="${API}/api/jobs/${j.job_id}/download?token=${token}">下载</a>`
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
