/**
 * HF Downloader - Web UI
 * Terminal Noir Edition
 */

(function() {
  'use strict';

  // =========================================
  // State
  // =========================================
  
  const state = {
    activeDownloads: new Map(),
    settings: {
      token: '',
      connections: 8,
      maxActive: 3,
      multipartThreshold: '32MiB',
      retries: 4,
      verify: 'size'
    },
    wsConnected: false,
    ws: null,
    pendingRender: false,
    lastRenderTime: 0
  };

  // Throttle render to max 10fps to avoid DOM thrashing
  const RENDER_INTERVAL = 100; // ms

  // =========================================
  // DOM Elements
  // =========================================

  const $ = (sel, ctx = document) => ctx.querySelector(sel);
  const $$ = (sel, ctx = document) => [...ctx.querySelectorAll(sel)];

  const elements = {
    connectionStatus: $('#connectionStatus'),
    navItems: $$('.nav-item'),
    pages: $$('.page'),
    downloadForm: $('#downloadForm'),
    activeDownloads: $('#activeDownloads'),
    globalProgress: $('#globalProgress'),
    globalBytes: $('#globalBytes'),
    globalSpeed: $('#globalSpeed'),
    globalEta: $('#globalEta'),
    activeCount: $('#activeCount'),
    queuedCount: $('#queuedCount'),
    completedCount: $('#completedCount'),
    previewPanel: $('#previewPanel'),
    previewContent: $('#previewContent'),
    toastContainer: $('#toastContainer'),
    scanlineCheck: $('#scanlineCheck'),
    accentSelect: $('#accentSelect')
  };

  // =========================================
  // Navigation
  // =========================================

  function initNavigation() {
    elements.navItems.forEach(item => {
      item.addEventListener('click', () => {
        const page = item.dataset.page;
        navigateTo(page);
      });
    });

    // Handle navigate links
    document.addEventListener('click', (e) => {
      const link = e.target.closest('[data-navigate]');
      if (link) {
        e.preventDefault();
        navigateTo(link.dataset.navigate);
      }
    });
  }

  function navigateTo(page) {
    // Update nav
    elements.navItems.forEach(item => {
      item.classList.toggle('active', item.dataset.page === page);
    });

    // Update pages
    elements.pages.forEach(p => {
      p.classList.toggle('active', p.id === `page-${page}`);
    });
  }

  // =========================================
  // WebSocket Connection
  // =========================================

  function connectWebSocket() {
    const protocol = location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${location.host}/api/ws`;

    try {
      state.ws = new WebSocket(wsUrl);

      state.ws.onopen = () => {
        state.wsConnected = true;
        updateConnectionStatus('connected');
        console.log('[WS] Connected');
      };

      state.ws.onclose = () => {
        state.wsConnected = false;
        updateConnectionStatus('disconnected');
        console.log('[WS] Disconnected, reconnecting in 3s...');
        setTimeout(connectWebSocket, 3000);
      };

      state.ws.onerror = (err) => {
        console.error('[WS] Error:', err);
      };

      state.ws.onmessage = (e) => {
        try {
          // Handle potential batched messages (newline separated)
          const messages = e.data.split('\n').filter(m => m.trim());
          for (const msg of messages) {
            const parsed = JSON.parse(msg);
            handleWSMessage(parsed);
          }
        } catch (err) {
          console.error('[WS] Parse error:', err, e.data);
        }
      };
    } catch (err) {
      console.error('[WS] Failed to connect:', err);
      updateConnectionStatus('disconnected');
      setTimeout(connectWebSocket, 3000);
    }
  }

  function updateConnectionStatus(status) {
    const el = elements.connectionStatus;
    el.className = `connection-status ${status}`;
    
    const text = el.querySelector('.status-text');
    switch (status) {
      case 'connected':
        text.textContent = 'Connected';
        break;
      case 'disconnected':
        text.textContent = 'Disconnected';
        break;
      default:
        text.textContent = 'Connecting...';
    }
  }

  // =========================================
  // WebSocket Message Handling
  // =========================================

  function handleWSMessage(msg) {
    console.log('[WS]', msg.type, msg.data);

    switch (msg.type) {
      case 'init':
        // Initial state with all jobs
        handleInit(msg.data);
        break;

      case 'job_update':
        // Job status update
        handleJobUpdate(msg.data);
        break;

      case 'event':
        // Legacy event format
        handleEvent(msg.data);
        break;

      default:
        console.log('[WS] Unknown message type:', msg.type);
    }
  }

  function handleInit(data) {
    console.log('[WS] Initialized with', data.jobs?.length || 0, 'jobs');
    
    // Clear and rebuild downloads from server state
    state.activeDownloads.clear();
    
    if (data.jobs) {
      for (const job of data.jobs) {
        handleJobUpdate(job);
      }
    }
    
    renderDownloads();
    updateStats();
  }

  function handleJobUpdate(job) {
    if (!job || !job.id) return;

    // Map server job to our download state
    const dl = {
      id: job.id,
      repo: job.repo,
      path: job.repo,
      total: job.progress?.totalBytes || 0,
      downloaded: job.progress?.downloadedBytes || 0,
      speed: job.progress?.bytesPerSecond || 0,
      status: mapJobStatus(job.status),
      files: job.files || [],
      totalFiles: job.progress?.totalFiles || 0,
      completedFiles: job.progress?.completedFiles || 0,
      error: job.error
    };

    state.activeDownloads.set(job.id, dl);
    
    // Throttled rendering to avoid DOM thrashing
    scheduleRender();

    // Show toast on status changes (always immediate)
    if (job.status === 'completed') {
      showToast(`Download complete: ${job.repo}`, 'success');
    } else if (job.status === 'failed') {
      showToast(`Download failed: ${job.error || job.repo}`, 'error');
    }
  }

  function scheduleRender() {
    if (state.pendingRender) return;
    
    const now = Date.now();
    const elapsed = now - state.lastRenderTime;
    
    if (elapsed >= RENDER_INTERVAL) {
      // Render immediately
      doRender();
    } else {
      // Schedule render
      state.pendingRender = true;
      setTimeout(() => {
        doRender();
      }, RENDER_INTERVAL - elapsed);
    }
  }

  function doRender() {
    state.pendingRender = false;
    state.lastRenderTime = Date.now();
    renderDownloads();
    updateStats();
    updateGlobalProgress();
  }

  function mapJobStatus(status) {
    switch (status) {
      case 'queued': return 'queued';
      case 'running': return 'active';
      case 'completed': return 'complete';
      case 'failed': return 'error';
      case 'cancelled': return 'cancelled';
      default: return 'pending';
    }
  }

  function handleEvent(event) {
    // Legacy event handling (kept for compatibility)
    console.log('[Event]', event);

    switch (event.event) {
      case 'scan_start':
        showToast(`Scanning ${event.repo}...`, 'info');
        break;

      case 'file_start':
        addOrUpdateDownload(event);
        break;

      case 'file_progress':
        updateDownloadProgress(event);
        break;

      case 'file_done':
        markDownloadComplete(event);
        break;

      case 'error':
        showToast(event.message || 'An error occurred', 'error');
        break;

      case 'done':
        showToast(event.message || 'Download complete!', 'success');
        updateStats();
        break;
    }
  }

  // =========================================
  // Downloads UI
  // =========================================

  function addOrUpdateDownload(event) {
    const id = event.path || event.repo;
    
    if (!state.activeDownloads.has(id)) {
      state.activeDownloads.set(id, {
        path: event.path,
        repo: event.repo,
        total: event.total || 0,
        downloaded: 0,
        speed: 0,
        status: 'active'
      });
    }

    renderDownloads();
    updateStats();
  }

  function updateDownloadProgress(event) {
    const id = event.path || event.repo;
    const dl = state.activeDownloads.get(id);
    
    if (dl) {
      dl.downloaded = event.downloaded || 0;
      dl.total = event.total || dl.total;
      dl.speed = event.speed || 0;
      renderDownloads();
    }

    updateGlobalProgress();
  }

  function markDownloadComplete(event) {
    const id = event.path || event.repo;
    const dl = state.activeDownloads.get(id);
    
    if (dl) {
      dl.status = event.message?.includes('skip') ? 'skipped' : 'complete';
      dl.downloaded = dl.total;
    }

    renderDownloads();
    updateStats();
  }

  function renderDownloads() {
    const container = elements.activeDownloads;
    
    if (state.activeDownloads.size === 0) {
      container.innerHTML = `
        <div class="empty-state">
          <div class="empty-icon">📭</div>
          <p>No active downloads</p>
          <div class="empty-actions">
            <a href="#" class="btn btn-primary" data-navigate="models">Download Model</a>
            <a href="#" class="btn btn-ghost" data-navigate="datasets">Download Dataset</a>
          </div>
        </div>
      `;
      return;
    }

    let html = '';
    state.activeDownloads.forEach((dl, id) => {
      const progress = dl.total > 0 ? (dl.downloaded / dl.total * 100).toFixed(1) : 0;
      
      // Status icons and classes
      let statusIcon, statusClass;
      switch (dl.status) {
        case 'active':
          statusIcon = '▶';
          statusClass = 'active';
          break;
        case 'complete':
          statusIcon = '✓';
          statusClass = 'complete';
          break;
        case 'error':
          statusIcon = '✕';
          statusClass = 'error';
          break;
        case 'cancelled':
          statusIcon = '⊘';
          statusClass = 'cancelled';
          break;
        case 'queued':
          statusIcon = '◎';
          statusClass = 'queued';
          break;
        default:
          statusIcon = '•';
          statusClass = 'pending';
      }

      // File progress info
      const fileInfo = dl.totalFiles > 0 
        ? `${dl.completedFiles}/${dl.totalFiles} files`
        : '';

      // Build per-file progress HTML (like the TUI)
      let filesHtml = '';
      if (dl.files && dl.files.length > 0) {
        filesHtml = '<div class="file-list">';
        dl.files.forEach(file => {
          const fileProgress = file.totalBytes > 0 ? (file.downloaded / file.totalBytes * 100) : 0;
          const fileName = file.path.split('/').pop(); // Just the filename
          
          let fileStatusIcon, fileStatusClass;
          switch (file.status) {
            case 'active':
              fileStatusIcon = '▶';
              fileStatusClass = 'downloading';
              break;
            case 'complete':
              fileStatusIcon = '✓';
              fileStatusClass = 'done';
              break;
            default:
              fileStatusIcon = '○';
              fileStatusClass = 'pending';
          }

          filesHtml += `
            <div class="file-item ${fileStatusClass}">
              <span class="file-status">${fileStatusIcon}</span>
              <span class="file-name" title="${escapeHtml(file.path)}">${escapeHtml(fileName)}</span>
              <div class="file-progress-bar">
                <div class="file-progress-fill ${file.status === 'active' ? 'animated' : ''}" style="width: ${fileProgress}%"></div>
              </div>
              <span class="file-size">${formatBytes(file.downloaded)}/${formatBytes(file.totalBytes)}</span>
            </div>
          `;
        });
        filesHtml += '</div>';
      }

      html += `
        <div class="download-item ${dl.status === 'active' ? 'is-active' : ''}" data-job-id="${escapeHtml(id)}">
          <div class="download-header">
            <div class="download-status ${statusClass}">${statusIcon}</div>
            <div class="download-info">
              <div class="download-name">${escapeHtml(dl.repo || dl.path)}</div>
              <div class="download-meta">
                ${formatBytes(dl.downloaded)} / ${formatBytes(dl.total)}
                ${dl.speed > 0 ? `• ${formatBytes(dl.speed)}/s` : ''}
                ${fileInfo ? `• ${fileInfo}` : ''}
              </div>
            </div>
            <div class="download-progress">
              <div class="progress-bar-container">
                <div class="progress-bar ${dl.status === 'active' ? 'animated' : ''}" style="--progress: ${progress}%">
                  ${dl.status === 'active' ? '<div class="progress-glow"></div>' : ''}
                </div>
              </div>
            </div>
            <div class="download-actions">
              ${dl.status === 'active' || dl.status === 'queued' 
                ? `<button class="btn btn-icon btn-cancel" data-job-id="${escapeHtml(id)}" title="Cancel">×</button>`
                : `<button class="btn btn-icon btn-remove" data-job-id="${escapeHtml(id)}" title="Remove">🗑</button>`
              }
            </div>
          </div>
          ${filesHtml}
        </div>
      `;
    });

    container.innerHTML = html;

    // Attach cancel handlers
    container.querySelectorAll('.btn-cancel').forEach(btn => {
      btn.addEventListener('click', () => cancelJob(btn.dataset.jobId));
    });

    // Attach remove handlers
    container.querySelectorAll('.btn-remove').forEach(btn => {
      btn.addEventListener('click', () => removeJob(btn.dataset.jobId));
    });
  }

  function removeJob(jobId) {
    // Remove from local state (completed jobs are just cleared from display)
    state.activeDownloads.delete(jobId);
    renderDownloads();
    updateStats();
    updateGlobalProgress();
  }

  async function cancelJob(jobId) {
    try {
      const res = await fetch(`/api/jobs/${jobId}`, { method: 'DELETE' });
      if (res.ok) {
        showToast('Download cancelled', 'warning');
      }
    } catch (err) {
      console.error('Failed to cancel job:', err);
    }
  }

  function updateGlobalProgress() {
    let totalBytes = 0;
    let downloadedBytes = 0;
    let totalSpeed = 0;

    state.activeDownloads.forEach(dl => {
      totalBytes += dl.total;
      downloadedBytes += dl.downloaded;
      if (dl.status === 'active') {
        totalSpeed += dl.speed;
      }
    });

    const progress = totalBytes > 0 ? (downloadedBytes / totalBytes * 100) : 0;
    
    elements.globalProgress.style.setProperty('--progress', `${progress}%`);
    elements.globalBytes.textContent = `${formatBytes(downloadedBytes)} / ${formatBytes(totalBytes)}`;
    elements.globalSpeed.textContent = totalSpeed > 0 ? `${formatBytes(totalSpeed)}/s` : '—';
    
    if (totalSpeed > 0 && totalBytes > downloadedBytes) {
      const remaining = (totalBytes - downloadedBytes) / totalSpeed;
      elements.globalEta.textContent = formatDuration(remaining);
    } else {
      elements.globalEta.textContent = '—';
    }
  }

  function updateStats() {
    let active = 0, complete = 0;
    
    state.activeDownloads.forEach(dl => {
      if (dl.status === 'active') active++;
      else complete++;
    });

    elements.activeCount.textContent = active;
    elements.completedCount.textContent = complete;
  }

  // =========================================
  // Download Forms (Models & Datasets)
  // =========================================

  function initDownloadForms() {
    // Model form
    const modelForm = $('#modelForm');
    if (modelForm) {
      modelForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        await startDownload(modelForm, false);
      });
    }

    // Dataset form
    const datasetForm = $('#datasetForm');
    if (datasetForm) {
      datasetForm.addEventListener('submit', async (e) => {
        e.preventDefault();
        await startDownload(datasetForm, true);
      });
    }

    // Preview buttons
    $$('.btn-preview').forEach(btn => {
      btn.addEventListener('click', async () => {
        const formId = btn.dataset.form;
        const form = $(`#${formId}`);
        if (form) {
          const isDataset = form.dataset.type === 'dataset';
          await previewDownload(form, isDataset);
        }
      });
    });

    // Close preview buttons
    $$('.btn-close-preview').forEach(btn => {
      btn.addEventListener('click', () => {
        btn.closest('.preview-panel').style.display = 'none';
      });
    });
  }

  async function startDownload(form, isDataset) {
    const formData = new FormData(form);
    const payload = formDataToObject(formData);
    payload.dataset = isDataset;
    
    try {
      const res = await fetch('/api/download', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      const data = await res.json();

      if (!res.ok) {
        throw new Error(data.error || 'Failed to start download');
      }

      // Check if it was an existing job
      if (data.message === 'Download already in progress') {
        showToast('Download already in progress', 'warning');
      } else {
        showToast('Download started!', 'success');
      }
      
      navigateTo('dashboard');
    } catch (err) {
      showToast(err.message, 'error');
    }
  }

  async function previewDownload(form, isDataset) {
    const formData = new FormData(form);
    const payload = formDataToObject(formData);
    payload.dataset = isDataset;
    payload.dryRun = true;

    try {
      const res = await fetch('/api/plan', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload)
      });

      if (!res.ok) {
        const err = await res.json();
        throw new Error(err.error || 'Failed to preview');
      }

      const plan = await res.json();
      renderPreview(form, plan);
    } catch (err) {
      showToast(err.message, 'error');
    }
  }

  function renderPreview(form, plan) {
    const files = plan.files || [];
    const previewPanel = form.closest('.download-form-container').querySelector('.preview-panel');
    const previewContent = previewPanel.querySelector('.preview-content');
    
    let html = `
      <table class="preview-table">
        <thead>
          <tr>
            <th>File</th>
            <th>Size</th>
            <th>Type</th>
          </tr>
        </thead>
        <tbody>
    `;

    files.forEach(f => {
      html += `
        <tr>
          <td>${escapeHtml(f.path)}</td>
          <td>${formatBytes(f.size)}</td>
          <td>${f.lfs ? 'LFS' : 'Regular'}</td>
        </tr>
      `;
    });

    html += `
        </tbody>
      </table>
      <div style="margin-top: 1rem; color: var(--text-secondary);">
        Total: ${files.length} files, ${formatBytes(files.reduce((a, f) => a + f.size, 0))}
      </div>
    `;

    previewContent.innerHTML = html;
    previewPanel.style.display = 'block';
  }

  // =========================================
  // Settings
  // =========================================

  function initSettings() {
    // Load settings from localStorage
    const saved = localStorage.getItem('hfdownloader_settings');
    if (saved) {
      try {
        Object.assign(state.settings, JSON.parse(saved));
        applySettings();
      } catch (e) {}
    }

    // Scanline toggle
    if (elements.scanlineCheck) {
      elements.scanlineCheck.checked = !$('.scanlines').classList.contains('hidden');
      elements.scanlineCheck.addEventListener('change', (e) => {
        $('.scanlines').classList.toggle('hidden', !e.target.checked);
        localStorage.setItem('hfdownloader_scanlines', e.target.checked);
      });

      // Load preference
      const scanPref = localStorage.getItem('hfdownloader_scanlines');
      if (scanPref !== null) {
        const enabled = scanPref === 'true';
        elements.scanlineCheck.checked = enabled;
        $('.scanlines').classList.toggle('hidden', !enabled);
      }
    }

    // Accent color
    if (elements.accentSelect) {
      elements.accentSelect.addEventListener('change', (e) => {
        document.body.dataset.accent = e.target.value;
        localStorage.setItem('hfdownloader_accent', e.target.value);
      });

      // Load preference
      const accentPref = localStorage.getItem('hfdownloader_accent');
      if (accentPref) {
        elements.accentSelect.value = accentPref;
        document.body.dataset.accent = accentPref;
      }
    }

    // Save button
    const saveBtn = $('#saveSettingsBtn');
    if (saveBtn) {
      saveBtn.addEventListener('click', saveSettings);
    }
  }

  function applySettings() {
    // Apply to form fields
    const tokenInput = $('#tokenInput');
    if (tokenInput) tokenInput.value = state.settings.token || '';

    const connectionsInput = $('#connectionsInput');
    if (connectionsInput) connectionsInput.value = state.settings.connections;

    const maxActiveInput = $('#maxActiveInput');
    if (maxActiveInput) maxActiveInput.value = state.settings.maxActive;

    const thresholdInput = $('#thresholdInput');
    if (thresholdInput) thresholdInput.value = state.settings.multipartThreshold;

    const retriesInput = $('#retriesInput');
    if (retriesInput) retriesInput.value = state.settings.retries;

    const verifySelect = $('#verifySelect');
    if (verifySelect) verifySelect.value = state.settings.verify;
  }

  async function saveSettings() {
    const newSettings = {
      token: $('#tokenInput')?.value || '',
      connections: parseInt($('#connectionsInput')?.value) || 8,
      maxActive: parseInt($('#maxActiveInput')?.value) || 3,
      multipartThreshold: $('#thresholdInput')?.value || '32MiB',
      retries: parseInt($('#retriesInput')?.value) || 4,
      verify: $('#verifySelect')?.value || 'size'
    };

    Object.assign(state.settings, newSettings);
    localStorage.setItem('hfdownloader_settings', JSON.stringify(state.settings));

    // Send to server
    try {
      await fetch('/api/settings', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(newSettings)
      });
      showToast('Settings saved!', 'success');
    } catch (err) {
      showToast('Failed to save settings to server', 'warning');
    }
  }

  // =========================================
  // Toasts
  // =========================================

  function showToast(message, type = 'info') {
    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    
    const icons = {
      success: '✓',
      error: '✕',
      warning: '⚠',
      info: 'ℹ'
    };

    toast.innerHTML = `
      <span class="toast-icon">${icons[type] || icons.info}</span>
      <span class="toast-message">${escapeHtml(message)}</span>
    `;

    elements.toastContainer.appendChild(toast);

    setTimeout(() => {
      toast.style.animation = 'slideIn 0.3s ease reverse forwards';
      setTimeout(() => toast.remove(), 300);
    }, 4000);
  }

  // =========================================
  // Utilities
  // =========================================

  function formatBytes(bytes) {
    if (bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  function formatDuration(seconds) {
    if (!seconds || seconds < 0) return '—';
    if (seconds < 60) return `${Math.round(seconds)}s`;
    if (seconds < 3600) {
      const m = Math.floor(seconds / 60);
      const s = Math.round(seconds % 60);
      return `${m}m ${s}s`;
    }
    const h = Math.floor(seconds / 3600);
    const m = Math.round((seconds % 3600) / 60);
    return `${h}h ${m}m`;
  }

  function escapeHtml(str) {
    if (!str) return '';
    return str
      .replace(/&/g, '&amp;')
      .replace(/</g, '&lt;')
      .replace(/>/g, '&gt;')
      .replace(/"/g, '&quot;');
  }

  function formDataToObject(formData) {
    const obj = {};
    formData.forEach((value, key) => {
      if (key === 'dataset' || key === 'appendFilterSubdir') {
        obj[key] = true;
      } else if (key === 'filters' || key === 'excludes') {
        // Convert comma-separated string to array
        if (value && value.trim()) {
          obj[key] = value.split(',').map(s => s.trim()).filter(s => s);
        }
      } else if (value) {
        obj[key] = value;
      }
    });
    return obj;
  }

  // =========================================
  // Password Toggle
  // =========================================

  function initPasswordToggles() {
    $$('.toggle-password').forEach(btn => {
      btn.addEventListener('click', () => {
        const input = btn.previousElementSibling;
        const isPassword = input.type === 'password';
        input.type = isPassword ? 'text' : 'password';
      });
    });
  }

  // =========================================
  // Init
  // =========================================

  function init() {
    initNavigation();
    initDownloadForms();
    initSettings();
    initPasswordToggles();

    // Connect WebSocket
    // Wait a bit for the page to settle
    setTimeout(connectWebSocket, 500);

    console.log('🤗 HF Downloader UI initialized');
  }

  // Start when DOM is ready
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

})();

