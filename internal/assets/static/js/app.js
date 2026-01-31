/**
 * HF Downloader - Modern Web UI
 */

(function() {
  'use strict';

  // =========================================
  // State
  // =========================================

  const state = {
    jobs: new Map(),
    settings: {},
    wsConnected: false,
    ws: null,
    currentPage: 'analyze'
  };

  // =========================================
  // DOM Elements
  // =========================================

  const $ = (sel) => document.querySelector(sel);
  const $$ = (sel) => document.querySelectorAll(sel);

  // =========================================
  // Navigation
  // =========================================

  function initNavigation() {
    $$('.nav-item').forEach(item => {
      item.addEventListener('click', (e) => {
        e.preventDefault();
        const page = item.dataset.page;
        navigateTo(page);
      });
    });
  }

  function navigateTo(page) {
    // Update nav
    $$('.nav-item').forEach(n => n.classList.remove('active'));
    $(`.nav-item[data-page="${page}"]`)?.classList.add('active');

    // Update page
    $$('.page').forEach(p => p.classList.remove('active'));
    $(`#page-${page}`)?.classList.add('active');

    state.currentPage = page;

    // Load page data
    if (page === 'cache') loadCache();
    if (page === 'jobs') loadJobs();
    if (page === 'settings') loadSettings();
    if (page === 'mirror') loadMirrorTargets();
  }

  // =========================================
  // WebSocket
  // =========================================

  function initWebSocket() {
    const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
    const wsUrl = `${protocol}//${window.location.host}/api/ws`;

    try {
      state.ws = new WebSocket(wsUrl);

      state.ws.onopen = () => {
        state.wsConnected = true;
        updateConnectionStatus(true);
      };

      state.ws.onclose = () => {
        state.wsConnected = false;
        updateConnectionStatus(false);
        // Reconnect after 3 seconds
        setTimeout(initWebSocket, 3000);
      };

      state.ws.onmessage = (event) => {
        try {
          const msg = JSON.parse(event.data);
          handleWSMessage(msg);
        } catch (e) {
          console.error('WS parse error:', e);
        }
      };

      state.ws.onerror = (error) => {
        console.error('WS error:', error);
      };
    } catch (e) {
      console.error('WS connection failed:', e);
      setTimeout(initWebSocket, 3000);
    }
  }

  function updateConnectionStatus(connected) {
    const indicator = $('.status-indicator');
    const text = $('.status-text');

    if (connected) {
      indicator?.classList.add('connected');
      if (text) text.textContent = 'Connected';
    } else {
      indicator?.classList.remove('connected');
      if (text) text.textContent = 'Reconnecting...';
    }
  }

  function handleWSMessage(msg) {
    if (msg.type === 'init') {
      // Initial state with all jobs
      const jobs = msg.data?.jobs || [];
      state.jobs.clear();
      jobs.forEach(job => {
        state.jobs.set(job.id, job);
      });
      updateJobsBadge();
      if (state.currentPage === 'jobs') {
        renderJobs();
      }
    } else if (msg.type === 'job_update') {
      // Job update - data contains the full job object
      const job = msg.data;
      if (job && job.id) {
        state.jobs.set(job.id, job);
        updateJobsBadge();
        if (state.currentPage === 'jobs') {
          renderJobs();
        }
      }
    }
  }

  function updateJobsBadge() {
    const activeCount = Array.from(state.jobs.values())
      .filter(j => j.status === 'running' || j.status === 'queued' || j.status === 'paused').length;

    const badge = $('#jobsBadge');
    if (badge) {
      if (activeCount > 0) {
        badge.textContent = activeCount;
        badge.style.display = 'block';
      } else {
        badge.style.display = 'none';
      }
    }
  }

  // =========================================
  // API Helpers
  // =========================================

  async function api(method, path, body = null) {
    const opts = {
      method,
      headers: { 'Content-Type': 'application/json' }
    };
    if (body) opts.body = JSON.stringify(body);

    const res = await fetch(`/api${path}`, opts);
    const data = await res.json();

    if (!res.ok) {
      throw new Error(data.error || 'API error');
    }
    return data;
  }

  // =========================================
  // Analyze Page
  // =========================================

  function initAnalyzePage() {
    const input = $('#analyzeInput');
    const btn = $('#analyzeBtn');

    // Enter key
    input?.addEventListener('keypress', (e) => {
      if (e.key === 'Enter') analyzeRepo();
    });

    // Button click
    btn?.addEventListener('click', analyzeRepo);

    // Example buttons
    $$('.example-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        if (input) input.value = btn.dataset.repo;
        // Pass 'dataset' type if specified on button
        const forceType = btn.dataset.type || null;
        analyzeRepo(forceType);
      });
    });
  }

  // Store current analysis for wizard
  let currentAnalysis = null;
  let hasShownRevisionPicker = false; // Track if we've shown picker for this repo

  async function analyzeRepo(forceType = null, revision = null) {
    const input = $('#analyzeInput');
    const resultDiv = $('#analyzeResult');
    const isDataset = forceType === 'dataset'; // Only set if user explicitly selected dataset

    const repo = input?.value.trim();
    if (!repo) {
      showToast('Please enter a repository', 'error');
      return;
    }

    // Reset revision picker flag when analyzing a new repo
    if (!revision) {
      hasShownRevisionPicker = false;
    }

    // Show loading
    resultDiv.innerHTML = `
      <div class="loading-state">
        <div class="spinner"></div>
        <p>Analyzing ${repo}${revision && revision !== 'main' ? ` (${revision})` : ''}...</p>
      </div>
    `;

    try {
      let queryParams = [];
      if (forceType) queryParams.push(`dataset=${forceType === 'dataset'}`);
      if (revision) queryParams.push(`revision=${encodeURIComponent(revision)}`);
      const queryString = queryParams.length > 0 ? `?${queryParams.join('&')}` : '';

      const data = await api('GET', `/analyze/${repo}${queryString}`);

      // Check if we need user to select model vs dataset
      if (data.needsSelection) {
        renderTypeSelection(data);
        return;
      }

      // Check if there are multiple refs and we haven't shown the picker yet
      if (data.refs && data.refs.length > 1 && !hasShownRevisionPicker && !revision) {
        hasShownRevisionPicker = true;
        showRevisionPicker(data);
        return;
      }

      currentAnalysis = data;
      renderAnalysisResult(data);
    } catch (e) {
      resultDiv.innerHTML = `
        <div class="empty-state">
          <div class="empty-icon" style="color: var(--color-error);">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="64" height="64">
              <circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/>
            </svg>
          </div>
          <h3>Analysis Failed</h3>
          <p>${escapeHtml(e.message)}</p>
        </div>
      `;
    }
  }

  // Show revision picker when multiple refs exist
  function showRevisionPicker(data) {
    const branches = data.refs.filter(r => r.type === 'branch');
    const tags = data.refs.filter(r => r.type === 'tag');

    let branchesHtml = '';
    if (branches.length > 0) {
      branchesHtml = `
        <div class="ref-group">
          <h5>Branches</h5>
          <div class="ref-list">
            ${branches.map(b => `
              <button class="ref-btn ${b.name === 'main' ? 'ref-default' : ''}" onclick="selectRevision('${escapeHtml(b.name)}', ${data.is_dataset})">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                  <line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>
                </svg>
                ${escapeHtml(b.name)}
                ${b.name === 'main' ? '<span class="ref-badge">default</span>' : ''}
              </button>
            `).join('')}
          </div>
        </div>
      `;
    }

    let tagsHtml = '';
    if (tags.length > 0) {
      tagsHtml = `
        <div class="ref-group">
          <h5>Tags</h5>
          <div class="ref-list">
            ${tags.slice(0, 10).map(t => `
              <button class="ref-btn" onclick="selectRevision('${escapeHtml(t.name)}', ${data.is_dataset})">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                  <path d="M20.59 13.41l-7.17 7.17a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z"/><line x1="7" y1="7" x2="7.01" y2="7"/>
                </svg>
                ${escapeHtml(t.name)}
              </button>
            `).join('')}
            ${tags.length > 10 ? `<div class="ref-more">... and ${tags.length - 10} more tags</div>` : ''}
          </div>
        </div>
      `;
    }

    showModal('Select Revision', `
      <p style="margin-bottom: 16px; color: var(--color-text-secondary);">
        This repository has multiple versions. Select which one to analyze:
      </p>
      ${branchesHtml}
      ${tagsHtml}
      <div class="form-actions" style="margin-top: 20px;">
        <button class="btn btn-ghost" onclick="hideModal(); selectRevision('main', ${data.is_dataset})">Use default (main)</button>
      </div>
    `);
  }

  // Handle revision selection
  window.selectRevision = function(revision, isDataset) {
    hideModal();
    const forceType = isDataset ? 'dataset' : null;
    analyzeRepo(forceType, revision);
  };

  // Show revision picker from analysis result (user clicked "change")
  window.showRevisionPickerFromAnalysis = function() {
    if (currentAnalysis && currentAnalysis.refs) {
      hasShownRevisionPicker = false; // Allow showing picker again
      showRevisionPicker(currentAnalysis);
    }
  };

  // Render type selection when both model and dataset exist
  function renderTypeSelection(data) {
    const resultDiv = $('#analyzeResult');
    resultDiv.innerHTML = `
      <div class="analysis-card">
        <div class="analysis-header">
          <div class="analysis-repo">${escapeHtml(data.repo)}</div>
          <span class="analysis-type" style="background: var(--color-warning);">Selection Required</span>
        </div>
        <div class="analysis-body">
          <div class="analysis-section">
            <h4>${escapeHtml(data.message)}</h4>
            <div style="display: flex; gap: 16px; margin-top: 20px;">
              <button class="btn btn-primary" onclick="analyzeRepo('model')">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20" style="margin-right: 8px;">
                  <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
                </svg>
                Analyze as Model
              </button>
              <button class="btn btn-secondary" onclick="analyzeRepo('dataset')">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20" style="margin-right: 8px;">
                  <ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/>
                </svg>
                Analyze as Dataset
              </button>
            </div>
          </div>
        </div>
      </div>
    `;
  }

  // Make analyzeRepo available globally for type selection buttons
  window.analyzeRepo = analyzeRepo;

  function renderAnalysisResult(data) {
    const resultDiv = $('#analyzeResult');
    if (!resultDiv) return;

    const filesHtml = data.files?.slice(0, 20).map(f => `
      <div class="analysis-file">
        <span class="analysis-file-name">${escapeHtml(f.path || f.name)}</span>
        <span class="analysis-file-size">${f.size_human || formatBytes(f.size)}</span>
      </div>
    `).join('') || '';

    const moreFiles = (data.files?.length || 0) > 20
      ? `<div class="analysis-file" style="justify-content: center; color: var(--color-text-muted);">
           ... and ${data.files.length - 20} more files
         </div>`
      : '';

    // Build type-specific info
    let typeInfoHtml = '';

    if (data.transformers) {
      const t = data.transformers;
      typeInfoHtml = `
        <div class="analysis-section">
          <h4>Model Configuration</h4>
          <div class="analysis-grid">
            ${t.architecture ? `<div class="analysis-stat"><div class="analysis-stat-label">Architecture</div><div class="analysis-stat-value">${escapeHtml(t.architecture)}</div></div>` : ''}
            ${t.estimated_parameters ? `<div class="analysis-stat"><div class="analysis-stat-label">Parameters</div><div class="analysis-stat-value">~${escapeHtml(t.estimated_parameters)}</div></div>` : ''}
            ${t.hidden_size ? `<div class="analysis-stat"><div class="analysis-stat-label">Hidden Size</div><div class="analysis-stat-value">${t.hidden_size}</div></div>` : ''}
            ${t.num_hidden_layers ? `<div class="analysis-stat"><div class="analysis-stat-label">Layers</div><div class="analysis-stat-value">${t.num_hidden_layers}</div></div>` : ''}
            ${t.context_length ? `<div class="analysis-stat"><div class="analysis-stat-label">Context Length</div><div class="analysis-stat-value">${t.context_length.toLocaleString()} tokens</div></div>` : ''}
            ${t.precision ? `<div class="analysis-stat"><div class="analysis-stat-label">Precision</div><div class="analysis-stat-value">${escapeHtml(t.precision)}</div></div>` : ''}
          </div>
        </div>
      `;
    }

    if (data.gguf) {
      const g = data.gguf;
      typeInfoHtml = `
        <div class="analysis-section">
          <h4>GGUF Information</h4>
          <div class="analysis-grid">
            ${g.model_name ? `<div class="analysis-stat"><div class="analysis-stat-label">Model</div><div class="analysis-stat-value">${escapeHtml(g.model_name)}</div></div>` : ''}
            ${g.parameter_count ? `<div class="analysis-stat"><div class="analysis-stat-label">Parameters</div><div class="analysis-stat-value">${escapeHtml(g.parameter_count)}</div></div>` : ''}
          </div>
        </div>
      `;
    }

    if (data.diffusers) {
      const d = data.diffusers;
      typeInfoHtml = `
        <div class="analysis-section">
          <h4>Diffusers Pipeline</h4>
          <div class="analysis-grid">
            ${d.pipeline_type ? `<div class="analysis-stat"><div class="analysis-stat-label">Pipeline</div><div class="analysis-stat-value">${escapeHtml(d.pipeline_type)}</div></div>` : ''}
            ${d.diffusers_version ? `<div class="analysis-stat"><div class="analysis-stat-label">Version</div><div class="analysis-stat-value">${escapeHtml(d.diffusers_version)}</div></div>` : ''}
            ${d.variants?.length ? `<div class="analysis-stat"><div class="analysis-stat-label">Variants</div><div class="analysis-stat-value">${d.variants.join(', ')}</div></div>` : ''}
          </div>
        </div>
      `;
    }

    if (data.dataset) {
      const ds = data.dataset;
      typeInfoHtml = `
        <div class="analysis-section">
          <h4>Dataset Information</h4>
          <div class="analysis-grid">
            ${ds.primary_format ? `<div class="analysis-stat"><div class="analysis-stat-label">Format</div><div class="analysis-stat-value">${escapeHtml(ds.primary_format)}</div></div>` : ''}
            ${ds.configs?.length ? `<div class="analysis-stat"><div class="analysis-stat-label">Configs</div><div class="analysis-stat-value">${ds.configs.join(', ')}</div></div>` : ''}
          </div>
        </div>
      `;
    }

    if (data.lora) {
      const l = data.lora;
      typeInfoHtml = `
        <div class="analysis-section">
          <h4>LoRA Adapter Information</h4>
          <div class="analysis-grid">
            ${l.adapter_type ? `<div class="analysis-stat"><div class="analysis-stat-label">Adapter Type</div><div class="analysis-stat-value">${escapeHtml(l.adapter_type)}</div></div>` : ''}
            ${l.rank ? `<div class="analysis-stat"><div class="analysis-stat-label">Rank (r)</div><div class="analysis-stat-value">${l.rank}</div></div>` : ''}
            ${l.alpha ? `<div class="analysis-stat"><div class="analysis-stat-label">Alpha</div><div class="analysis-stat-value">${l.alpha}</div></div>` : ''}
            ${l.base_model ? `<div class="analysis-stat"><div class="analysis-stat-label">Base Model</div><div class="analysis-stat-value">${escapeHtml(l.base_model)}</div></div>` : ''}
          </div>
        </div>
      `;
    }

    if (data.quantized) {
      const q = data.quantized;
      typeInfoHtml = `
        <div class="analysis-section">
          <h4>Quantized Model Information</h4>
          <div class="analysis-grid">
            ${q.method ? `<div class="analysis-stat"><div class="analysis-stat-label">Method</div><div class="analysis-stat-value">${escapeHtml(q.method.toUpperCase())}</div></div>` : ''}
            ${q.bits ? `<div class="analysis-stat"><div class="analysis-stat-label">Bits</div><div class="analysis-stat-value">${q.bits}-bit</div></div>` : ''}
            ${q.group_size ? `<div class="analysis-stat"><div class="analysis-stat-label">Group Size</div><div class="analysis-stat-value">${q.group_size}</div></div>` : ''}
            ${q.backends?.length ? `<div class="analysis-stat"><div class="analysis-stat-label">Backends</div><div class="analysis-stat-value">${q.backends.slice(0,3).join(', ')}</div></div>` : ''}
          </div>
        </div>
      `;
    }

    // Build unified selectable items section (works for all types)
    let selectableItemsHtml = '';
    const hasSelectableItems = data.selectable_items && data.selectable_items.length > 0;

    if (hasSelectableItems) {
      selectableItemsHtml = `
        <div class="analysis-section">
          <h4>Select Files to Download</h4>
          <p style="font-size: 13px; color: var(--color-text-muted); margin-bottom: 12px;">Choose which files you want to download:</p>
          ${renderSelectableItems(data.selectable_items, 'selectableItems')}
        </div>
      `;
    }

    // Build related downloads section (for LoRA base models, etc.)
    const relatedDownloadsHtml = renderRelatedDownloads(data.related_downloads);

    // Build the download command - prefer API-provided command, fall back to building it
    let baseCmd = data.cli_command_full || data.cli_command || `hfdownloader download ${data.repo}`;
    if (!data.cli_command) {
      if (data.is_dataset) {
        baseCmd += ' --dataset';
      }
      if (data.branch && data.branch !== 'main') {
        baseCmd += ` -b ${data.branch}`;
      }
    }

    // Build branch/revision display
    const branchDisplay = data.branch && data.branch !== 'main'
      ? `<span class="analysis-branch" title="Revision: ${escapeHtml(data.branch)}">
           <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
             <line x1="6" y1="3" x2="6" y2="15"/><circle cx="18" cy="6" r="3"/><circle cx="6" cy="18" r="3"/><path d="M18 9a9 9 0 0 1-9 9"/>
           </svg>
           ${escapeHtml(data.branch)}
         </span>`
      : '';

    // Show "Change" link if multiple refs available
    const changeRevisionLink = data.refs && data.refs.length > 1
      ? `<button class="btn-link" onclick="showRevisionPickerFromAnalysis()">change</button>`
      : '';

    resultDiv.innerHTML = `
      <div class="analysis-card">
        <div class="analysis-header">
          <div class="analysis-repo">${escapeHtml(data.repo)}${branchDisplay}${changeRevisionLink}</div>
          <span class="analysis-type">${escapeHtml(data.type_description || data.type)}</span>
          <div class="analysis-meta">
            <span>${data.file_count} files</span>
            <span>${data.total_size_human}</span>
          </div>
        </div>
        <div class="analysis-body">
          ${typeInfoHtml}
          ${selectableItemsHtml}
          ${relatedDownloadsHtml}
          <div class="analysis-section">
            <h4>Files <span id="selectedFilesCount" style="font-weight: normal; color: var(--color-text-muted);"></span></h4>
            <div class="analysis-files" id="analysisFilesList">
              ${filesHtml}
              ${moreFiles}
            </div>
          </div>
        </div>
        <div class="analysis-actions-wrapper">
          ${renderCLICommandBox(data.cli_command, baseCmd)}
          <div class="analysis-actions">
            <button class="btn btn-ghost" onclick="clearAnalysis()">
              Clear
            </button>
            <button class="btn btn-secondary" onclick="showAdvancedOptions()">
              Advanced Options
            </button>
            <button class="btn btn-primary" onclick="startWizardDownload('${escapeHtml(data.repo)}', ${data.is_dataset})">
              Download
            </button>
          </div>
        </div>
      </div>
    `;

    // Initialize selectable items event handlers
    if (hasSelectableItems) {
      initSelectableItems();
      updateCLICommandFromSelections();
    }
  }

  // Clear analysis and reset to initial state
  window.clearAnalysis = function() {
    currentAnalysis = null;
    advancedOptions = { filter: '', exclude: '' };
    const input = $('#analyzeInput');
    if (input) input.value = '';

    const resultDiv = $('#analyzeResult');
    if (resultDiv) {
      resultDiv.innerHTML = `
        <div class="empty-state">
          <div class="empty-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="64" height="64">
              <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
              <polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
              <line x1="12" y1="22.08" x2="12" y2="12"/>
            </svg>
          </div>
          <h3>Analyze Model or Dataset</h3>
          <p>Enter a HuggingFace model or dataset ID - we'll auto-detect the type and show files, size, and download options.</p>
          <div class="example-repos">
            <span class="example-label">GGUF:</span>
            <button class="example-btn" data-repo="TheBloke/Mistral-7B-Instruct-v0.2-GGUF">Mistral-7B-GGUF</button>
            <button class="example-btn" data-repo="bartowski/Qwen2.5-7B-Instruct-GGUF">Qwen2.5-7B-GGUF</button>
          </div>
          <div class="example-repos">
            <span class="example-label">Diffusers:</span>
            <button class="example-btn" data-repo="stabilityai/stable-diffusion-xl-base-1.0">SDXL-base</button>
            <button class="example-btn" data-repo="runwayml/stable-diffusion-v1-5">SD-v1.5</button>
          </div>
          <div class="example-repos">
            <span class="example-label">LoRA/GPTQ:</span>
            <button class="example-btn" data-repo="predibase/glue_stsb">LoRA-adapter</button>
            <button class="example-btn" data-repo="TheBloke/Llama-2-7B-Chat-GPTQ">Llama-2-GPTQ</button>
          </div>
          <div class="example-repos">
            <span class="example-label">Datasets:</span>
            <button class="example-btn" data-repo="HuggingFaceFW/fineweb-edu" data-type="dataset">FineWeb-Edu</button>
            <button class="example-btn" data-repo="OpenAssistant/oasst1" data-type="dataset">OpenAssistant</button>
          </div>
        </div>
      `;
      // Re-attach example button handlers
      $$('.example-btn').forEach(btn => {
        btn.addEventListener('click', () => {
          if (input) input.value = btn.dataset.repo;
          const forceType = btn.dataset.type || null;
          analyzeRepo(forceType);
        });
      });
    }
  };

  // Update the download command based on selected quantizations and advanced options
  function updateDownloadCommand() {
    const commandEl = $('#downloadCommand');
    if (!commandEl || !currentAnalysis) return;

    const selectedQuants = Array.from(document.querySelectorAll('#quantOptions input[type="checkbox"]:checked'))
      .map(cb => cb.dataset.filter);

    let cmd = `hfdownloader -r ${currentAnalysis.repo}`;

    // Add dataset flag
    if (currentAnalysis.is_dataset) {
      cmd += ' -d';
    }

    // Add revision if not main (from analysis)
    if (currentAnalysis.branch && currentAnalysis.branch !== 'main') {
      cmd += ` -b ${currentAnalysis.branch}`;
    }

    // Add filters - either from GGUF selection or advanced options
    if (selectedQuants.length > 0 && selectedQuants.length < (currentAnalysis.gguf?.quantizations?.length || 0)) {
      cmd += ` -f "${selectedQuants.join(',')}"`;
    } else if (advancedOptions.filter) {
      cmd += ` -f "${advancedOptions.filter}"`;
    }

    // Add excludes
    if (advancedOptions.exclude) {
      cmd += ` -e "${advancedOptions.exclude}"`;
    }

    commandEl.textContent = cmd;
  }

  // Copy command to clipboard
  window.copyCommand = function() {
    const commandEl = $('#downloadCommand');
    if (commandEl) {
      navigator.clipboard.writeText(commandEl.textContent);
      showToast('Command copied to clipboard', 'success');
    }
  };

  // Store advanced options (filter/exclude only - revision comes from analysis)
  let advancedOptions = {
    filter: '',
    exclude: ''
  };

  // Show advanced options modal
  window.showAdvancedOptions = function() {
    if (!currentAnalysis) return;

    showModal('Advanced Options', `
      <div class="form-group">
        <label for="advFilter">File Filter (comma-separated)</label>
        <input type="text" id="advFilter" value="${escapeHtml(advancedOptions.filter)}" placeholder="e.g., q4_k_m,q5_k_m">
        <p class="form-hint">Only download files matching these patterns</p>
      </div>
      <div class="form-group">
        <label for="advExclude">Exclude Filter (comma-separated)</label>
        <input type="text" id="advExclude" value="${escapeHtml(advancedOptions.exclude)}" placeholder="e.g., fp16,bf16">
        <p class="form-hint">Skip files matching these patterns</p>
      </div>
      <div class="form-actions">
        <button class="btn btn-secondary" onclick="hideModal()">Cancel</button>
        <button class="btn btn-primary" onclick="applyAdvancedOptions()">Apply</button>
      </div>
    `);
  };

  // Apply advanced options and update command preview
  window.applyAdvancedOptions = function() {
    advancedOptions.filter = $('#advFilter')?.value || '';
    advancedOptions.exclude = $('#advExclude')?.value || '';

    hideModal();
    updateDownloadCommand();
    showToast('Options applied', 'success');
  };

  // Start download from wizard with selected options
  window.startWizardDownload = async function(repo, isDataset) {
    // Get selected items from unified selector (new) or legacy quantOptions
    let selectedItems = Array.from(document.querySelectorAll('.selectable-items input[type="checkbox"]:checked'))
      .map(cb => cb.value);

    // Fallback to legacy GGUF selector if no new selectable items
    if (selectedItems.length === 0) {
      selectedItems = Array.from(document.querySelectorAll('#quantOptions input[type="checkbox"]:checked'))
        .map(cb => cb.dataset.filter);
    }

    // Build filters - prefer selections, fallback to advanced options
    let filters = [];
    const totalItems = document.querySelectorAll('.selectable-items input[type="checkbox"], #quantOptions input[type="checkbox"]').length;

    // Only add filter if user selected a subset (not all)
    if (selectedItems.length > 0 && selectedItems.length < totalItems) {
      filters = selectedItems;
    } else if (advancedOptions.filter) {
      filters = advancedOptions.filter.split(',').map(s => s.trim()).filter(Boolean);
    }

    // Build excludes from advanced options
    const excludes = advancedOptions.exclude
      ? advancedOptions.exclude.split(',').map(s => s.trim()).filter(Boolean)
      : [];

    try {
      const body = {
        repo,
        revision: currentAnalysis?.branch || 'main',
        dataset: isDataset,
        filters,
        excludes
      };

      await api('POST', '/download', body);
      showToast(`Download started: ${repo}`, 'success');
      navigateTo('jobs');
    } catch (e) {
      showToast(`Failed: ${e.message}`, 'error');
    }
  };

  // Make downloadFromAnalysis available globally
  window.downloadFromAnalysis = function(repo, isDataset) {
    if (isDataset) {
      $('#datasetRepo').value = repo;
      navigateTo('download');
    } else {
      $('#modelRepo').value = repo;
      navigateTo('download');
    }
  };

  // =========================================
  // Download Page
  // =========================================

  function initDownloadPage() {
    // Model form
    $('#modelForm')?.addEventListener('submit', async (e) => {
      e.preventDefault();
      await startDownload('model');
    });

    // Dataset form
    $('#datasetForm')?.addEventListener('submit', async (e) => {
      e.preventDefault();
      await startDownload('dataset');
    });

    // Preview buttons
    $('#previewModelBtn')?.addEventListener('click', () => previewDownload('model'));
    $('#previewDatasetBtn')?.addEventListener('click', () => previewDownload('dataset'));
  }

  async function startDownload(type) {
    const isDataset = type === 'dataset';
    const prefix = isDataset ? 'dataset' : 'model';

    const repo = $(`#${prefix}Repo`)?.value.trim();
    const revision = $(`#${prefix}Revision`)?.value.trim() || 'main';
    const filter = $(`#${prefix}Filter`)?.value.trim();
    const exclude = $(`#${prefix}Exclude`)?.value.trim();

    if (!repo) {
      showToast('Please enter a repository', 'error');
      return;
    }

    const body = {
      repo,
      revision,
      dataset: isDataset,
      filters: filter ? filter.split(',').map(s => s.trim()).filter(Boolean) : [],
      excludes: exclude ? exclude.split(',').map(s => s.trim()).filter(Boolean) : []
    };

    try {
      const data = await api('POST', '/download', body);
      showToast(`Download started: ${repo}`, 'success');
      navigateTo('jobs');
    } catch (e) {
      showToast(`Failed: ${e.message}`, 'error');
    }
  }

  async function previewDownload(type) {
    const isDataset = type === 'dataset';
    const prefix = isDataset ? 'dataset' : 'model';

    const repo = $(`#${prefix}Repo`)?.value.trim();
    if (!repo) {
      showToast('Please enter a repository', 'error');
      return;
    }

    const body = {
      repo,
      revision: $(`#${prefix}Revision`)?.value.trim() || 'main',
      dataset: isDataset,
      filters: ($(`#${prefix}Filter`)?.value || '').split(',').map(s => s.trim()).filter(Boolean),
      excludes: ($(`#${prefix}Exclude`)?.value || '').split(',').map(s => s.trim()).filter(Boolean),
      dryRun: true
    };

    try {
      showModal('Preview', '<div class="loading-state"><div class="spinner"></div><p>Scanning repository...</p></div>');

      const data = await api('POST', '/plan', body);

      const filesHtml = data.files?.map(f => `
        <div class="analysis-file">
          <span class="analysis-file-name">${escapeHtml(f.path)}</span>
          <span class="analysis-file-size">${formatBytes(f.size)}</span>
        </div>
      `).join('') || '<p>No files found</p>';

      setModalContent(`
        <p style="margin-bottom: 16px; color: var(--color-text-secondary);">
          ${data.totalFiles} files, ${formatBytes(data.totalSize)} total
        </p>
        <div class="analysis-files" style="max-height: 400px;">
          ${filesHtml}
        </div>
      `);
    } catch (e) {
      setModalContent(`<p style="color: var(--color-error);">${escapeHtml(e.message)}</p>`);
    }
  }

  // =========================================
  // Jobs Page
  // =========================================

  async function loadJobs() {
    try {
      const data = await api('GET', '/jobs');
      state.jobs.clear();
      (data.jobs || []).forEach(job => {
        state.jobs.set(job.id, job);
      });
      renderJobs();
      updateJobsBadge();
    } catch (e) {
      console.error('Failed to load jobs:', e);
    }
  }

  function renderJobs() {
    const container = $('#jobsList');
    if (!container) return;

    const jobs = Array.from(state.jobs.values());

    if (jobs.length === 0) {
      container.innerHTML = `
        <div class="empty-state">
          <div class="empty-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="64" height="64">
              <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>
            </svg>
          </div>
          <h3>No Active Downloads</h3>
          <p>Start a download from the Download page to see progress here.</p>
        </div>
      `;
      return;
    }

    container.innerHTML = jobs.map(job => {
      const p = job.progress || {};
      const totalBytes = p.totalBytes || 0;
      const downloadedBytes = p.downloadedBytes || 0;
      const progress = totalBytes > 0 ? (downloadedBytes / totalBytes * 100) : 0;
      const speed = p.bytesPerSecond || 0;
      const status = job.status || 'queued';

      // Determine which action buttons to show
      const isRunning = status === 'running';
      const isQueued = status === 'queued';
      const isPaused = status === 'paused';
      const isDone = status === 'completed' || status === 'failed' || status === 'cancelled';

      let actionButtons = '';
      if (isRunning) {
        actionButtons = `
          <button class="btn btn-sm btn-warning" onclick="pauseJob('${escapeHtml(job.id)}')">Pause</button>
          <button class="btn btn-sm btn-danger" onclick="cancelJob('${escapeHtml(job.id)}')">Cancel</button>
        `;
      } else if (isPaused) {
        actionButtons = `
          <button class="btn btn-sm btn-primary" onclick="resumeJob('${escapeHtml(job.id)}')">Resume</button>
          <button class="btn btn-sm btn-danger" onclick="cancelJob('${escapeHtml(job.id)}')">Cancel</button>
        `;
      } else if (isQueued) {
        actionButtons = `<button class="btn btn-sm btn-danger" onclick="cancelJob('${escapeHtml(job.id)}')">Cancel</button>`;
      } else if (isDone) {
        actionButtons = `<button class="btn btn-sm btn-secondary" onclick="dismissJob('${escapeHtml(job.id)}')">Dismiss</button>`;
      }

      return `
        <div class="job-card">
          <div class="job-header">
            <div>
              <div class="job-repo">${escapeHtml(job.repo)}</div>
              <div style="font-size: 13px; color: var(--color-text-muted);">${escapeHtml(job.revision || 'main')}</div>
            </div>
            <div class="job-header-right">
              <span class="job-status ${status}">${status}</span>
              ${actionButtons}
            </div>
          </div>
          <div class="job-progress">
            <div class="progress-bar">
              <div class="progress-fill" style="width: ${progress}%"></div>
            </div>
          </div>
          <div class="job-stats">
            <span>${progress.toFixed(1)}%</span>
            ${speed > 0 ? `<span>${formatBytes(speed)}/s</span>` : ''}
            <span>${formatBytes(downloadedBytes)} / ${formatBytes(totalBytes)}</span>
            <span>${p.completedFiles || 0} / ${p.totalFiles || 0} files</span>
          </div>
          ${job.error ? `<div class="job-error">${escapeHtml(job.error)}</div>` : ''}
        </div>
      `;
    }).join('');
  }

  // Cancel a running/queued job
  window.cancelJob = async function(jobId) {
    try {
      await api('DELETE', `/jobs/${jobId}`);
      showToast('Download cancelled', 'success');
      // Update local state immediately
      const job = state.jobs.get(jobId);
      if (job) {
        job.status = 'cancelled';
        state.jobs.set(jobId, job);
        renderJobs();
        updateJobsBadge();
      }
    } catch (e) {
      showToast(`Failed to cancel: ${e.message}`, 'error');
    }
  };

  // Pause a running job
  window.pauseJob = async function(jobId) {
    try {
      await api('POST', `/jobs/${jobId}/pause`);
      showToast('Download paused', 'success');
      const job = state.jobs.get(jobId);
      if (job) {
        job.status = 'paused';
        state.jobs.set(jobId, job);
        renderJobs();
        updateJobsBadge();
      }
    } catch (e) {
      showToast(`Failed to pause: ${e.message}`, 'error');
    }
  };

  // Resume a paused job
  window.resumeJob = async function(jobId) {
    try {
      await api('POST', `/jobs/${jobId}/resume`);
      showToast('Download resumed', 'success');
      const job = state.jobs.get(jobId);
      if (job) {
        job.status = 'queued';
        state.jobs.set(jobId, job);
        renderJobs();
        updateJobsBadge();
      }
    } catch (e) {
      showToast(`Failed to resume: ${e.message}`, 'error');
    }
  };

  // Dismiss (remove from view) a completed/failed/cancelled job
  window.dismissJob = function(jobId) {
    state.jobs.delete(jobId);
    renderJobs();
    updateJobsBadge();
  };

  // =========================================
  // Cache Page
  // =========================================

  let cacheData = { repos: [], stats: {}, cacheDir: '' };
  let cacheFilter = 'all';
  let cacheSort = 'name';
  let cacheView = 'grid';
  let cacheSearch = '';

  async function loadCache() {
    const container = $('#cacheList');
    const statsContainer = $('#cacheStats');
    if (!container) return;

    container.innerHTML = `
      <div class="loading-state">
        <div class="spinner"></div>
        <p>Loading cache...</p>
      </div>
    `;

    try {
      cacheData = await api('GET', '/cache');
      updateCacheStats();
      renderCacheList();
    } catch (e) {
      container.innerHTML = `
        <div class="empty-state">
          <h3>Failed to Load Cache</h3>
          <p>${escapeHtml(e.message)}</p>
        </div>
      `;
    }
  }

  function updateCacheStats() {
    const stats = cacheData.stats || {};
    $('#statModels').textContent = stats.totalModels || 0;
    $('#statDatasets').textContent = stats.totalDatasets || 0;
    $('#statSize').textContent = stats.totalSizeHuman || '0 B';
    $('#statFiles').textContent = stats.totalFiles || 0;
  }

  function renderCacheList() {
    const container = $('#cacheList');
    if (!container) return;

    let repos = [...(cacheData.repos || [])];

    // Apply filter
    if (cacheFilter !== 'all') {
      repos = repos.filter(r => r.type === cacheFilter);
    }

    // Apply search
    if (cacheSearch) {
      const search = cacheSearch.toLowerCase();
      repos = repos.filter(r =>
        r.repo.toLowerCase().includes(search) ||
        r.owner.toLowerCase().includes(search) ||
        r.name.toLowerCase().includes(search)
      );
    }

    // Apply sort
    repos.sort((a, b) => {
      switch (cacheSort) {
        case 'size':
          return b.size - a.size;
        case 'date':
          return (b.downloaded || '').localeCompare(a.downloaded || '');
        default:
          return a.repo.localeCompare(b.repo);
      }
    });

    if (repos.length === 0) {
      const message = cacheData.repos?.length === 0
        ? 'No models or datasets downloaded yet.'
        : 'No repositories match your filters.';
      container.innerHTML = `
        <div class="empty-state">
          <div class="empty-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="64" height="64">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
            </svg>
          </div>
          <h3>${cacheData.repos?.length === 0 ? 'Cache is Empty' : 'No Results'}</h3>
          <p>${message}</p>
          ${cacheData.cacheDir ? `<p class="cache-dir-hint">Cache: ${escapeHtml(cacheData.cacheDir)}</p>` : ''}
        </div>
      `;
      return;
    }

    // Render based on view mode
    container.className = `cache-list cache-${cacheView}-view`;

    if (cacheView === 'grid') {
      container.innerHTML = repos.map(repo => renderCacheCard(repo)).join('');
    } else {
      container.innerHTML = `
        <div class="cache-table">
          <div class="cache-table-header">
            <div class="cache-col-type">Type</div>
            <div class="cache-col-repo">Repository</div>
            <div class="cache-col-size">Size</div>
            <div class="cache-col-files">Files</div>
            <div class="cache-col-date">Downloaded</div>
            <div class="cache-col-actions"></div>
          </div>
          ${repos.map(repo => renderCacheRow(repo)).join('')}
        </div>
      `;
    }
  }

  function renderCacheCard(repo) {
    const typeIcon = repo.type === 'model'
      ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
           <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
         </svg>`
      : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
           <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/>
           <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>
         </svg>`;

    // Build status badge based on download status
    let statusBadge = '';
    if (repo.downloadStatus === 'complete') {
      statusBadge = '<span class="cache-badge cache-badge-complete" title="Fully downloaded with hfdownloader">Complete</span>';
    } else if (repo.downloadStatus === 'filtered') {
      const filterTitle = repo.manifest?.filters ? `Filtered download: ${repo.manifest.filters}` : 'Partial download (filters applied)';
      statusBadge = `<span class="cache-badge cache-badge-filtered" title="${escapeHtml(filterTitle)}">Filtered</span>`;
    } else if (repo.manifest) {
      statusBadge = '<span class="cache-badge cache-badge-manifest" title="Has manifest file">Tracked</span>';
    }

    return `
      <div class="cache-card" onclick="showCacheDetails('${escapeHtml(repo.repo)}', '${escapeHtml(repo.type)}')">
        <div class="cache-card-header">
          <span class="cache-card-type cache-type-${repo.type}">
            ${typeIcon}
            ${repo.type}
          </span>
          ${statusBadge}
        </div>
        <div class="cache-card-body">
          <div class="cache-card-owner">${escapeHtml(repo.owner)}</div>
          <div class="cache-card-name">${escapeHtml(repo.name)}</div>
        </div>
        <div class="cache-card-meta">
          <div class="cache-card-stat">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
              <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
              <polyline points="3.27 6.96 12 12.01 20.73 6.96"/>
              <line x1="12" y1="22.08" x2="12" y2="12"/>
            </svg>
            ${escapeHtml(repo.sizeHuman)}
          </div>
          <div class="cache-card-stat">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
              <path d="M13 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V9z"/>
              <polyline points="13 2 13 9 20 9"/>
            </svg>
            ${repo.fileCount} files
          </div>
        </div>
        <div class="cache-card-footer">
          ${repo.commit ? `<code class="cache-commit" title="Commit: ${escapeHtml(repo.commit)}">${escapeHtml(repo.commit)}</code>` : ''}
          ${repo.downloaded ? `<span class="cache-date">${escapeHtml(repo.downloaded)}</span>` : ''}
        </div>
      </div>
    `;
  }

  function renderCacheRow(repo) {
    const typeIcon = repo.type === 'model'
      ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
           <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
         </svg>`
      : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
           <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/>
           <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>
         </svg>`;

    // Build status badge
    let statusBadge = '';
    if (repo.downloadStatus === 'complete') {
      statusBadge = '<span class="cache-badge cache-badge-complete">Complete</span>';
    } else if (repo.downloadStatus === 'filtered') {
      statusBadge = '<span class="cache-badge cache-badge-filtered">Filtered</span>';
    } else if (repo.manifest) {
      statusBadge = '<span class="cache-badge cache-badge-manifest">Tracked</span>';
    }

    return `
      <div class="cache-table-row" onclick="showCacheDetails('${escapeHtml(repo.repo)}', '${escapeHtml(repo.type)}')">
        <div class="cache-col-type">
          <span class="cache-type-badge cache-type-${repo.type}">
            ${typeIcon}
            ${repo.type}
          </span>
        </div>
        <div class="cache-col-repo">
          <span class="cache-repo-name">${escapeHtml(repo.repo)}</span>
          ${statusBadge}
        </div>
        <div class="cache-col-size">${escapeHtml(repo.sizeHuman)}</div>
        <div class="cache-col-files">${repo.fileCount}</div>
        <div class="cache-col-date">${escapeHtml(repo.downloaded || '-')}</div>
        <div class="cache-col-actions">
          <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation(); showCacheDetails('${escapeHtml(repo.repo)}', '${escapeHtml(repo.type)}')" title="View details">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
              <circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>
            </svg>
          </button>
        </div>
      </div>
    `;
  }

  window.showCacheDetails = async function(repo, type) {
    try {
      showModal('Repository Details', '<div class="loading-state"><div class="spinner"></div></div>');
      const data = await api('GET', `/cache/${repo}`);

      const typeIcon = data.type === 'model'
        ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20">
             <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
           </svg>`
        : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20">
             <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/>
             <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>
           </svg>`;

      // Build files table
      const filesHtml = data.files?.length > 0
        ? `<div class="cache-detail-files">
             <h4>Files (${data.files.length})</h4>
             <div class="cache-files-list">
               ${data.files.slice(0, 20).map(f => `
                 <div class="cache-file-row">
                   <span class="cache-file-name" title="${escapeHtml(f.name)}">
                     ${f.isLfs ? '<span class="lfs-badge">LFS</span>' : ''}
                     ${escapeHtml(f.name)}
                   </span>
                   <span class="cache-file-size">${escapeHtml(f.sizeHuman)}</span>
                 </div>
               `).join('')}
               ${data.files.length > 20 ? `<div class="cache-files-more">... and ${data.files.length - 20} more files</div>` : ''}
             </div>
           </div>`
        : '';

      // Build download status badge for detail view
      let statusBadgeHtml = '';
      if (data.downloadStatus === 'complete') {
        statusBadgeHtml = '<span class="cache-badge cache-badge-complete">Complete</span>';
      } else if (data.downloadStatus === 'filtered') {
        statusBadgeHtml = '<span class="cache-badge cache-badge-filtered">Filtered</span>';
      } else if (data.downloadStatus === 'unknown') {
        statusBadgeHtml = '<span class="cache-badge cache-badge-unknown">External</span>';
      }

      // Build manifest info
      const manifestHtml = data.manifest
        ? `<div class="cache-detail-section">
             <h4>Download Info</h4>
             <div class="cache-detail-grid">
               <div class="cache-detail-item">
                 <span class="cache-detail-label">Status</span>
                 <span class="cache-detail-value">
                   ${data.downloadStatus === 'complete' ? '✓ Complete download' : ''}
                   ${data.downloadStatus === 'filtered' ? '◐ Filtered download' : ''}
                   ${data.manifest.filters ? ` (${escapeHtml(data.manifest.filters)})` : ''}
                 </span>
               </div>
               <div class="cache-detail-item">
                 <span class="cache-detail-label">Downloaded</span>
                 <span class="cache-detail-value">${escapeHtml(data.manifest.downloaded)}</span>
               </div>
               ${data.manifest.command ? `
                 <div class="cache-detail-item cache-detail-full">
                   <span class="cache-detail-label">Command</span>
                   <code class="cache-detail-code">${escapeHtml(data.manifest.command)}</code>
                 </div>
               ` : ''}
             </div>
           </div>`
        : (data.downloadStatus === 'unknown' ? `<div class="cache-detail-section">
             <h4>Download Info</h4>
             <div class="cache-detail-note">
               <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                 <circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>
               </svg>
               <span>Downloaded by external tool (Python transformers, diffusers, etc.)</span>
             </div>
           </div>` : '');

      setModalContent(`
        <div class="cache-detail-modal">
          <div class="cache-detail-header">
            <span class="cache-detail-type cache-type-${data.type}">
              ${typeIcon}
              ${data.type}
            </span>
            <div class="cache-detail-repo">
              <span class="cache-detail-owner">${escapeHtml(data.owner)}/</span>
              <span class="cache-detail-name">${escapeHtml(data.name)}</span>
            </div>
          </div>

          <div class="cache-detail-stats">
            <div class="cache-detail-stat">
              <div class="cache-detail-stat-value">${escapeHtml(data.sizeHuman)}</div>
              <div class="cache-detail-stat-label">Total Size</div>
            </div>
            <div class="cache-detail-stat">
              <div class="cache-detail-stat-value">${data.fileCount}</div>
              <div class="cache-detail-stat-label">Files</div>
            </div>
            <div class="cache-detail-stat">
              <div class="cache-detail-stat-value">${escapeHtml(data.branch || 'main')}</div>
              <div class="cache-detail-stat-label">Branch</div>
            </div>
            ${data.commit ? `
              <div class="cache-detail-stat">
                <div class="cache-detail-stat-value"><code>${escapeHtml(data.commit)}</code></div>
                <div class="cache-detail-stat-label">Commit</div>
              </div>
            ` : ''}
          </div>

          ${manifestHtml}

          <div class="cache-detail-section">
            <h4>Paths</h4>
            <div class="cache-detail-paths">
              <div class="cache-path-item">
                <span class="cache-path-label">Cache (HF format)</span>
                <code class="cache-path-value">${escapeHtml(data.path)}</code>
              </div>
              ${data.friendlyPath ? `
                <div class="cache-path-item">
                  <span class="cache-path-label">Friendly view</span>
                  <code class="cache-path-value">${escapeHtml(data.friendlyPath)}</code>
                </div>
              ` : ''}
            </div>
          </div>

          ${filesHtml}

          <div class="cache-detail-actions">
            <button class="btn btn-danger" onclick="confirmDeleteCache('${escapeHtml(data.repo)}', '${escapeHtml(data.type)}')">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                <line x1="10" y1="11" x2="10" y2="17"/><line x1="14" y1="11" x2="14" y2="17"/>
              </svg>
              Delete
            </button>
            <a href="https://huggingface.co/${data.type === 'dataset' ? 'datasets/' : ''}${data.repo}" target="_blank" class="btn btn-secondary">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                <path d="M18 13v6a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V8a2 2 0 0 1 2-2h6"/>
                <polyline points="15 3 21 3 21 9"/><line x1="10" y1="14" x2="21" y2="3"/>
              </svg>
              View on HuggingFace
            </a>
            <button class="btn btn-ghost" onclick="hideModal()">Close</button>
          </div>
        </div>
      `);
    } catch (e) {
      setModalContent(`<p style="color: var(--color-error);">${escapeHtml(e.message)}</p>`);
    }
  };

  // Rebuild cache (regenerate friendly view symlinks)
  async function rebuildCache() {
    const btn = $('#rebuildCacheBtn');
    if (btn) {
      btn.disabled = true;
      btn.innerHTML = `
        <div class="spinner" style="width: 18px; height: 18px;"></div>
        Rebuilding...
      `;
    }

    try {
      const result = await api('POST', '/cache/rebuild', { clean: true });

      // Show result modal
      showModal('Rebuild Complete', `
        <div class="rebuild-result">
          <div class="rebuild-success">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="48" height="48">
              <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
              <polyline points="22 4 12 14.01 9 11.01"/>
            </svg>
          </div>
          <p class="rebuild-message">${escapeHtml(result.message)}</p>
          <div class="rebuild-stats">
            <div class="rebuild-stat">
              <span class="rebuild-stat-value">${result.reposScanned}</span>
              <span class="rebuild-stat-label">Repos Scanned</span>
            </div>
            <div class="rebuild-stat">
              <span class="rebuild-stat-value">${result.symlinksCreated}</span>
              <span class="rebuild-stat-label">Links Created</span>
            </div>
            <div class="rebuild-stat">
              <span class="rebuild-stat-value">${result.symlinksUpdated}</span>
              <span class="rebuild-stat-label">Links Updated</span>
            </div>
            <div class="rebuild-stat">
              <span class="rebuild-stat-value">${result.orphansRemoved || 0}</span>
              <span class="rebuild-stat-label">Orphans Removed</span>
            </div>
          </div>
          ${result.errors?.length > 0 ? `
            <div class="rebuild-errors">
              <h5>Errors:</h5>
              <ul>${result.errors.map(e => `<li>${escapeHtml(e)}</li>`).join('')}</ul>
            </div>
          ` : ''}
          <div class="form-actions" style="margin-top: 20px;">
            <button class="btn btn-primary" onclick="hideModal()">Done</button>
          </div>
        </div>
      `);

      // Refresh cache list
      loadCache();
    } catch (e) {
      showToast(`Rebuild failed: ${e.message}`, 'error');
    } finally {
      if (btn) {
        btn.disabled = false;
        btn.innerHTML = `
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="18" height="18">
            <path d="M19.73 14.87A8 8 0 1 1 12 4"/>
            <path d="M12 4V1l4 4-4 4V4"/>
          </svg>
          Rebuild
        `;
      }
    }
  }

  // Delete a cached repo with confirmation
  window.confirmDeleteCache = function(repo, type) {
    showModal('Delete from Cache', `
      <div class="delete-confirm">
        <div class="delete-warning">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="48" height="48">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
            <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
          </svg>
        </div>
        <p class="delete-message">Are you sure you want to delete <strong>${escapeHtml(repo)}</strong> from the cache?</p>
        <p class="delete-note">This will permanently remove all cached files for this ${type}. This action cannot be undone.</p>
        <div class="form-actions" style="margin-top: 20px;">
          <button class="btn btn-ghost" onclick="hideModal()">Cancel</button>
          <button class="btn btn-danger" onclick="deleteCache('${escapeHtml(repo)}', '${escapeHtml(type)}')">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
              <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
            </svg>
            Delete
          </button>
        </div>
      </div>
    `);
  };

  // Actually delete the cache
  window.deleteCache = async function(repo, type) {
    try {
      await api('DELETE', `/cache/${repo}?type=${type}`);
      hideModal();
      showToast(`Deleted ${repo} from cache`, 'success');
      loadCache(); // Refresh the list
    } catch (e) {
      showToast(`Failed to delete: ${e.message}`, 'error');
    }
  };

  function initCachePage() {
    // Refresh button
    $('#refreshCacheBtn')?.addEventListener('click', loadCache);

    // Rebuild button
    $('#rebuildCacheBtn')?.addEventListener('click', rebuildCache);

    // Search
    const searchInput = $('#cacheSearch');
    if (searchInput) {
      let searchTimeout;
      searchInput.addEventListener('input', () => {
        clearTimeout(searchTimeout);
        searchTimeout = setTimeout(() => {
          cacheSearch = searchInput.value.trim();
          renderCacheList();
        }, 200);
      });
    }

    // Filter buttons
    $$('.cache-filters .filter-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        $$('.cache-filters .filter-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        cacheFilter = btn.dataset.filter;
        renderCacheList();
      });
    });

    // Sort dropdown
    const sortSelect = $('#cacheSort');
    if (sortSelect) {
      sortSelect.addEventListener('change', () => {
        cacheSort = sortSelect.value;
        renderCacheList();
      });
    }

    // View toggle
    $$('.cache-view-toggle .view-btn').forEach(btn => {
      btn.addEventListener('click', () => {
        $$('.cache-view-toggle .view-btn').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        cacheView = btn.dataset.view;
        renderCacheList();
      });
    });
  }

  // =========================================
  // Settings Page
  // =========================================

  async function loadSettings() {
    try {
      const data = await api('GET', '/settings');
      state.settings = data;

      // Display cache directory (read-only)
      const cacheDirEl = $('#cacheDir');
      if (cacheDirEl) {
        cacheDirEl.textContent = data.cacheDir || '~/.cache/huggingface';
      }

      // Display config file paths
      const configPathEl = $('#settingsConfigPath');
      if (configPathEl && data.configFile) {
        configPathEl.innerHTML = `Config: <code>${escapeHtml(data.configFile)}</code>`;
        if (data.targetsFile) {
          configPathEl.innerHTML += ` &nbsp;|&nbsp; Targets: <code>${escapeHtml(data.targetsFile)}</code>`;
        }
      }

      $('#hfToken').value = data.token || '';
      $('#connections').value = data.connections || 8;
      $('#maxActive').value = data.maxActive || 3;
      $('#retries').value = data.retries || 4;
      $('#verify').value = data.verify || 'size';
      $('#endpoint').value = data.endpoint || '';

      // Load proxy settings
      if (data.proxy) {
        $('#proxyUrl').value = data.proxy.url || '';
        $('#proxyUsername').value = data.proxy.username || '';
        $('#proxyPassword').value = ''; // Never show saved password
        $('#proxyNoProxy').value = data.proxy.noProxy || '';
        $('#proxyNoEnvProxy').checked = data.proxy.noEnvProxy || false;
      } else {
        $('#proxyUrl').value = '';
        $('#proxyUsername').value = '';
        $('#proxyPassword').value = '';
        $('#proxyNoProxy').value = '';
        $('#proxyNoEnvProxy').checked = false;
      }
    } catch (e) {
      console.error('Failed to load settings:', e);
    }
  }

  function initSettingsPage() {
    $('#saveSettingsBtn')?.addEventListener('click', saveSettings);

    // Toggle password visibility
    $$('.toggle-visibility').forEach(btn => {
      btn.addEventListener('click', () => {
        const target = btn.dataset.target;
        const input = $(`#${target}`);
        if (input) {
          const isPassword = input.type === 'password';
          input.type = isPassword ? 'text' : 'password';
          btn.querySelector('.icon-show').style.display = isPassword ? 'none' : 'block';
          btn.querySelector('.icon-hide').style.display = isPassword ? 'block' : 'none';
        }
      });
    });
  }

  async function saveSettings() {
    const body = {
      token: $('#hfToken')?.value || '',
      connections: parseInt($('#connections')?.value) || 8,
      maxActive: parseInt($('#maxActive')?.value) || 3,
      retries: parseInt($('#retries')?.value) || 4,
      verify: $('#verify')?.value || 'size',
      endpoint: $('#endpoint')?.value || ''
    };

    // Add proxy settings if URL is provided
    const proxyUrl = $('#proxyUrl')?.value || '';
    if (proxyUrl || $('#proxyNoEnvProxy')?.checked) {
      body.proxy = {
        url: proxyUrl,
        username: $('#proxyUsername')?.value || '',
        noProxy: $('#proxyNoProxy')?.value || '',
        noEnvProxy: $('#proxyNoEnvProxy')?.checked || false
      };
      // Only send password if it was changed (not empty)
      const proxyPassword = $('#proxyPassword')?.value;
      if (proxyPassword) {
        body.proxy.password = proxyPassword;
      }
    }

    try {
      const result = await api('POST', '/settings', body);
      showToast(result.message || 'Settings saved', 'success');
      // Clear password field after save
      if ($('#proxyPassword')) {
        $('#proxyPassword').value = '';
      }
    } catch (e) {
      showToast(`Failed: ${e.message}`, 'error');
    }
  }

  // =========================================
  // Mirror Page
  // =========================================

  let mirrorData = { targets: [], localStats: null, diffResults: null };
  let diffFilter = 'all';
  let diffSearch = '';

  function initMirrorPage() {
    $('#addTargetBtn')?.addEventListener('click', showAddTargetModal);
    $('#refreshMirrorBtn')?.addEventListener('click', () => loadMirrorTargets());
    $('#mirrorDiffBtn')?.addEventListener('click', runMirrorDiff);
    $('#mirrorPushBtn')?.addEventListener('click', () => runMirrorSync('push'));
    $('#mirrorPullBtn')?.addEventListener('click', () => runMirrorSync('pull'));

    // Diff filter buttons
    $$('[data-diff-filter]').forEach(btn => {
      btn.addEventListener('click', () => {
        $$('[data-diff-filter]').forEach(b => b.classList.remove('active'));
        btn.classList.add('active');
        diffFilter = btn.dataset.diffFilter;
        renderDiffResults(mirrorData.diffResults);
      });
    });

    // Diff search
    $('#diffSearch')?.addEventListener('input', (e) => {
      diffSearch = e.target.value.toLowerCase();
      renderDiffResults(mirrorData.diffResults);
    });
  }

  async function loadMirrorTargets() {
    const container = $('#targetsList');
    if (container) {
      container.innerHTML = `
        <div class="loading-state">
          <div class="spinner"></div>
          <p>Loading targets...</p>
        </div>
      `;
    }

    try {
      const result = await api('GET', '/mirror/targets');
      mirrorData.targets = result.targets || [];
      renderMirrorTargets(mirrorData.targets);
      updateTargetSelect(mirrorData.targets);
      updateMirrorStats(mirrorData.targets);

      // Also load local cache stats
      try {
        const cacheResult = await api('GET', '/cache');
        mirrorData.localStats = cacheResult.stats;
        updateMirrorStats(mirrorData.targets);
      } catch (e) {
        // Ignore cache stats error
      }
    } catch (e) {
      if (container) {
        container.innerHTML = `
          <div class="empty-state">
            <p>Failed to load targets: ${escapeHtml(e.message)}</p>
          </div>
        `;
      }
    }
  }

  function updateMirrorStats(targets) {
    const onlineCount = targets.filter(t => t.exists).length;
    const totalCount = targets.length;

    $('#statTargets').textContent = totalCount;
    $('#statOnline').textContent = `${onlineCount}/${totalCount}`;

    // Local cache size
    if (mirrorData.localStats) {
      $('#statLocalSize').textContent = mirrorData.localStats.totalSizeHuman || '-';
    }

    // Sync status
    const syncStatusEl = $('#statSyncStatus');
    const syncStatusCard = $('#syncStatusCard');
    if (mirrorData.diffResults) {
      const s = mirrorData.diffResults.summary;
      if (s.inSync) {
        syncStatusEl.textContent = 'In Sync';
        syncStatusCard?.classList.remove('out-of-sync');
        syncStatusCard?.classList.add('in-sync');
      } else {
        syncStatusEl.textContent = `${s.missing || 0} pending`;
        syncStatusCard?.classList.remove('in-sync');
        syncStatusCard?.classList.add('out-of-sync');
      }
    } else {
      syncStatusEl.textContent = 'Unknown';
      syncStatusCard?.classList.remove('in-sync', 'out-of-sync');
    }
  }

  function renderMirrorTargets(targets) {
    const container = $('#targetsList');
    if (!container) return;

    // Build the targets grid with cards
    let html = '';

    if (!targets || targets.length === 0) {
      // Empty state as a card
      html = `
        <div class="target-card target-card-add" onclick="showAddTargetModal()">
          <div class="target-card-add-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="24" height="24">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
          </div>
          <span class="target-card-add-text">Add your first mirror target</span>
        </div>
      `;
    } else {
      // Render target cards
      html = targets.map(t => `
        <div class="target-card ${t.exists ? 'target-online' : 'target-offline'}">
          <div class="target-card-header">
            <div class="target-card-title">
              <div class="target-card-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20">
                  ${getTargetIcon(t.name)}
                </svg>
              </div>
              <span class="target-card-name">${escapeHtml(t.name)}</span>
            </div>
            <div class="target-card-status ${t.exists ? 'online' : 'offline'}">
              <span class="target-card-status-dot"></span>
              ${t.exists ? 'Online' : 'Offline'}
            </div>
          </div>
          <div class="target-card-path">${escapeHtml(t.path)}</div>
          ${t.description ? `<div class="target-card-description">${escapeHtml(t.description)}</div>` : ''}
          <div class="target-card-footer">
            <div class="target-card-meta">
              ${t.repoCount !== undefined ? `
                <div class="target-card-meta-item">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
                    <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
                  </svg>
                  ${t.repoCount} repos
                </div>
              ` : ''}
              ${t.sizeHuman ? `
                <div class="target-card-meta-item">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
                    <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
                  </svg>
                  ${t.sizeHuman}
                </div>
              ` : ''}
            </div>
            <div class="target-card-actions">
              <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation(); selectTarget('${escapeHtml(t.name)}')" title="Select for sync">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                  <polyline points="9 11 12 14 22 4"/>
                  <path d="M21 12v7a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11"/>
                </svg>
              </button>
              <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation(); removeTarget('${escapeHtml(t.name)}')" title="Remove target">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
                  <polyline points="3 6 5 6 21 6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>
                </svg>
              </button>
            </div>
          </div>
        </div>
      `).join('');

      // Add the "Add Target" card at the end
      html += `
        <div class="target-card target-card-add" onclick="showAddTargetModal()">
          <div class="target-card-add-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="24" height="24">
              <line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/>
            </svg>
          </div>
          <span class="target-card-add-text">Add another target</span>
        </div>
      `;
    }

    container.innerHTML = html;
  }

  function getTargetIcon(name) {
    const lowerName = name.toLowerCase();
    if (lowerName.includes('nas') || lowerName.includes('network')) {
      return '<rect x="2" y="6" width="20" height="12" rx="2" ry="2"/><line x1="6" y1="10" x2="6" y2="14"/><line x1="10" y1="10" x2="10" y2="14"/>';
    } else if (lowerName.includes('usb') || lowerName.includes('drive') || lowerName.includes('external')) {
      return '<rect x="4" y="4" width="16" height="16" rx="2" ry="2"/><rect x="9" y="9" width="6" height="6"/><line x1="9" y1="2" x2="9" y2="4"/><line x1="15" y1="2" x2="15" y2="4"/>';
    } else if (lowerName.includes('office') || lowerName.includes('work') || lowerName.includes('server')) {
      return '<rect x="2" y="3" width="20" height="14" rx="2" ry="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/>';
    } else if (lowerName.includes('cloud') || lowerName.includes('remote')) {
      return '<path d="M18 10h-1.26A8 8 0 1 0 9 20h9a5 5 0 0 0 0-10z"/>';
    } else {
      // Default: sync/mirror icon
      return '<polyline points="16 3 21 3 21 8"/><line x1="4" y1="20" x2="21" y2="3"/><polyline points="21 16 21 21 16 21"/><line x1="15" y1="15" x2="21" y2="21"/>';
    }
  }

  window.selectTarget = function(name) {
    const select = $('#syncTarget');
    if (select) {
      select.value = name;
      // Scroll to sync control
      document.querySelector('.sync-control-panel')?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
  };

  function updateTargetSelect(targets) {
    const select = $('#syncTarget');
    if (!select) return;

    const currentValue = select.value;
    select.innerHTML = '<option value="">Select a target...</option>';

    targets.forEach(t => {
      const option = document.createElement('option');
      option.value = t.name;
      option.textContent = `${t.name} (${t.path})`;
      if (!t.exists) option.textContent += ' [offline]';
      select.appendChild(option);
    });

    if (currentValue) select.value = currentValue;
  }

  function showAddTargetModal() {
    showModal('Add Mirror Target', `
      <form id="addTargetForm" class="modal-form">
        <div class="form-group">
          <label for="targetName">Name</label>
          <input type="text" id="targetName" placeholder="e.g., office, usb, nas" required>
          <p class="form-hint">A short name to identify this target</p>
        </div>
        <div class="form-group">
          <label for="targetPath">Path</label>
          <input type="text" id="targetPath" placeholder="/path/to/hf/cache" required>
          <p class="form-hint">Absolute path to the HuggingFace cache directory</p>
        </div>
        <div class="form-group">
          <label for="targetDescription">Description (optional)</label>
          <input type="text" id="targetDescription" placeholder="e.g., Office NAS server">
        </div>
        <div class="form-actions">
          <button type="button" class="btn btn-ghost" onclick="hideModal()">Cancel</button>
          <button type="submit" class="btn btn-primary">Add Target</button>
        </div>
      </form>
    `);

    $('#addTargetForm')?.addEventListener('submit', async (e) => {
      e.preventDefault();
      const name = $('#targetName')?.value?.trim();
      const path = $('#targetPath')?.value?.trim();
      const description = $('#targetDescription')?.value?.trim();

      if (!name || !path) {
        showToast('Name and path are required', 'error');
        return;
      }

      try {
        await api('POST', '/mirror/targets', { name, path, description });
        hideModal();
        showToast(`Added target "${name}"`, 'success');
        loadMirrorTargets();
      } catch (e) {
        showToast(`Failed: ${e.message}`, 'error');
      }
    });
  }

  // Expose showAddTargetModal globally for onclick handlers
  window.showAddTargetModal = showAddTargetModal;

  window.removeTarget = async function(name) {
    if (!confirm(`Remove target "${name}"?`)) return;

    try {
      await api('DELETE', `/mirror/targets/${name}`);
      showToast(`Removed target "${name}"`, 'success');
      loadMirrorTargets();
    } catch (e) {
      showToast(`Failed: ${e.message}`, 'error');
    }
  };

  async function runMirrorDiff() {
    const target = $('#syncTarget')?.value;
    if (!target) {
      showToast('Please select a target', 'error');
      return;
    }

    const filter = $('#syncFilter')?.value?.trim() || '';
    const btn = $('#mirrorDiffBtn');

    try {
      btn.disabled = true;
      btn.innerHTML = '<span class="spinner-sm"></span> Comparing...';

      const result = await api('POST', '/mirror/diff', {
        target,
        repoFilter: filter
      });

      // Reset filters before showing new results
      diffFilter = 'all';
      diffSearch = '';
      $$('[data-diff-filter]').forEach(b => b.classList.remove('active'));
      $('[data-diff-filter="all"]')?.classList.add('active');
      const searchInput = $('#diffSearch');
      if (searchInput) searchInput.value = '';

      renderDiffResults(result);

      // Scroll to results
      $('#mirrorDiffSection')?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } catch (e) {
      showToast(`Failed: ${e.message}`, 'error');
    } finally {
      btn.disabled = false;
      btn.innerHTML = `
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="20" height="20">
          <circle cx="12" cy="12" r="10"/>
          <line x1="12" y1="8" x2="12" y2="12"/>
          <line x1="12" y1="16" x2="12.01" y2="16"/>
        </svg>
        Compare
      `;
    }
  }

  function renderDiffResults(result) {
    const section = $('#mirrorDiffSection');
    const container = $('#diffResults');
    const summary = $('#diffSummary');

    if (!section || !container) return;
    if (!result) {
      section.style.display = 'none';
      return;
    }

    section.style.display = 'block';

    // Store for filtering
    mirrorData.diffResults = result;

    const s = result.summary;

    // Update stats with sync status
    updateMirrorStats(mirrorData.targets);

    if (s.inSync) {
      summary.innerHTML = '<span class="badge badge-success">In Sync</span>';
      container.innerHTML = `
        <div class="empty-state" style="padding: 48px 24px;">
          <div class="empty-icon">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" width="64" height="64" style="color: var(--color-success);">
              <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/>
              <polyline points="22 4 12 14.01 9 11.01"/>
            </svg>
          </div>
          <h3>Caches are in sync</h3>
          <p>No differences found between local and target.</p>
        </div>
      `;
      return;
    }

    // Build summary badges
    summary.innerHTML = `
      ${s.missing > 0 ? `<span class="badge badge-warning">${s.missing} to push (${s.missingSizeHuman})</span>` : ''}
      ${s.extra > 0 ? `<span class="badge badge-info">${s.extra} extra on target</span>` : ''}
      ${s.outdated > 0 ? `<span class="badge badge-secondary">${s.outdated} outdated</span>` : ''}
    `;

    if (!result.diffs || result.diffs.length === 0) {
      container.innerHTML = '<p style="padding: 24px; text-align: center; color: var(--color-text-muted);">No differences found.</p>';
      return;
    }

    // Apply filters
    let diffs = [...result.diffs];

    // Filter by status
    if (diffFilter !== 'all') {
      diffs = diffs.filter(d => d.status === diffFilter);
    }

    // Filter by search
    if (diffSearch) {
      diffs = diffs.filter(d => d.repo.toLowerCase().includes(diffSearch));
    }

    if (diffs.length === 0) {
      container.innerHTML = `
        <div class="empty-state" style="padding: 48px 24px;">
          <h3>No matches</h3>
          <p>No repositories match your current filter.</p>
        </div>
      `;
      return;
    }

    // Render the diff items
    const typeIcon = (type) => type === 'model'
      ? `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
           <path d="M21 16V8a2 2 0 0 0-1-1.73l-7-4a2 2 0 0 0-2 0l-7 4A2 2 0 0 0 3 8v8a2 2 0 0 0 1 1.73l7 4a2 2 0 0 0 2 0l7-4A2 2 0 0 0 21 16z"/>
         </svg>`
      : `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
           <path d="M4 19.5A2.5 2.5 0 0 1 6.5 17H20"/>
           <path d="M6.5 2H20v20H6.5A2.5 2.5 0 0 1 4 19.5v-15A2.5 2.5 0 0 1 6.5 2z"/>
         </svg>`;

    const statusLabel = (status) => {
      switch (status) {
        case 'missing': return 'To Push';
        case 'extra': return 'Extra';
        case 'outdated': return 'Outdated';
        default: return status;
      }
    };

    container.innerHTML = `
      <div class="diff-results-header">
        <span>Status</span>
        <span>Type</span>
        <span>Repository</span>
        <span>Size</span>
        <span></span>
      </div>
      ${diffs.map(d => `
        <div class="diff-item">
          <div>
            <span class="diff-item-status ${d.status}">${statusLabel(d.status)}</span>
          </div>
          <div class="diff-item-type">
            ${typeIcon(d.type)}
            ${d.type}
          </div>
          <div class="diff-item-repo">${escapeHtml(d.repo)}</div>
          <div class="diff-item-size">${d.sizeHuman || '-'}</div>
          <div class="diff-item-action">
            ${d.status === 'missing' ? `
              <button class="btn btn-ghost btn-sm" onclick="event.stopPropagation();" title="Will be pushed">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="14" height="14">
                  <line x1="12" y1="19" x2="12" y2="5"/><polyline points="5 12 12 5 19 12"/>
                </svg>
              </button>
            ` : ''}
          </div>
        </div>
      `).join('')}
    `;
  }

  async function runMirrorSync(direction) {
    const target = $('#syncTarget')?.value;
    if (!target) {
      showToast('Please select a target', 'error');
      return;
    }

    const filter = $('#syncFilter')?.value?.trim() || '';
    const verify = $('#syncVerify')?.checked || false;
    const force = $('#syncForce')?.checked || false;
    const deleteExtra = $('#syncDelete')?.checked || false;

    const action = direction === 'push' ? 'Push to' : 'Pull from';
    if (!confirm(`${action} target "${target}"?\n\nThis will copy repos ${direction === 'push' ? 'from local to target' : 'from target to local'}.${deleteExtra ? '\n\nWARNING: Extra repos will be deleted!' : ''}`)) {
      return;
    }

    const btn = direction === 'push' ? $('#mirrorPushBtn') : $('#mirrorPullBtn');
    const originalHtml = btn.innerHTML;

    try {
      btn.disabled = true;
      btn.innerHTML = `<span class="spinner-sm"></span> ${direction === 'push' ? 'Pushing' : 'Pulling'}...`;

      const result = await api('POST', `/mirror/${direction}`, {
        target,
        repoFilter: filter,
        verify,
        force,
        deleteExtra,
        dryRun: false
      });

      if (result.success) {
        showToast(result.message, 'success');
      } else {
        showToast(result.message + (result.errors?.length ? ` (${result.errors.length} errors)` : ''), 'warning');
      }

      // Refresh diff after sync
      runMirrorDiff();
    } catch (e) {
      showToast(`Failed: ${e.message}`, 'error');
    } finally {
      btn.disabled = false;
      btn.innerHTML = originalHtml;
    }
  }

  // =========================================
  // Modal
  // =========================================

  function showModal(title, content) {
    $('#modalTitle').textContent = title;
    $('#modalBody').innerHTML = content;
    $('#modalBackdrop').classList.add('active');
  }

  function setModalContent(content) {
    $('#modalBody').innerHTML = content;
  }

  function hideModal() {
    $('#modalBackdrop').classList.remove('active');
  }

  // Expose hideModal globally for onclick handlers
  window.hideModal = hideModal;

  function initModal() {
    $('#modalClose')?.addEventListener('click', hideModal);
    $('#modalBackdrop')?.addEventListener('click', (e) => {
      if (e.target === $('#modalBackdrop')) hideModal();
    });

    // ESC key to close modals
    document.addEventListener('keydown', (e) => {
      if (e.key === 'Escape') {
        const modal = $('#modalBackdrop');
        if (modal?.classList.contains('active')) {
          hideModal();
        }
      }
    });
  }

  // =========================================
  // Toast
  // =========================================

  function showToast(message, type = 'info') {
    const container = $('#toastContainer');
    if (!container) return;

    const toast = document.createElement('div');
    toast.className = `toast ${type}`;
    toast.innerHTML = `<span class="toast-message">${escapeHtml(message)}</span>`;

    container.appendChild(toast);

    setTimeout(() => {
      toast.style.animation = 'slideIn 0.3s ease reverse';
      setTimeout(() => toast.remove(), 300);
    }, 4000);
  }

  // =========================================
  // Selectable Items Component
  // =========================================

  /**
   * Renders selectable items with checkboxes grouped by category.
   * Works for all model types: GGUF quantizations, Diffusers components, etc.
   */
  function renderSelectableItems(items, containerId) {
    if (!items || items.length === 0) return '';

    // Group items by category
    const categories = {};
    items.forEach(item => {
      const cat = item.category || 'default';
      if (!categories[cat]) categories[cat] = [];
      categories[cat].push(item);
    });

    const categoryTitles = {
      'quantization': 'Quantizations',
      'variant': 'Variants',
      'component': 'Components',
      'split': 'Dataset Splits',
      'format': 'Weight Format',
      'precision': 'Precision',
      'default': 'Options'
    };

    let html = `<div class="selectable-items" id="${containerId}">`;

    for (const [category, categoryItems] of Object.entries(categories)) {
      const title = categoryTitles[category] || category.charAt(0).toUpperCase() + category.slice(1);

      html += `
        <div class="selector-category">
          <h5 class="selector-category-title">${escapeHtml(title)}</h5>
          <div class="selector-items">`;

      categoryItems.forEach(item => {
        const stars = item.quality > 0 ? renderQualityStars(item.quality) : '';
        const recommended = item.recommended ? '<span class="badge-recommended">Recommended</span>' : '';
        const sizeInfo = item.size_human ? `<span class="selector-size">${escapeHtml(item.size_human)}</span>` : '';
        const ramInfo = item.ram_human ? `<span class="selector-ram">~${escapeHtml(item.ram_human)} RAM</span>` : '';

        html += `
          <label class="selector-item ${item.recommended ? 'selector-item-recommended' : ''}">
            <input type="checkbox"
                   value="${escapeHtml(item.filter_value)}"
                   data-id="${escapeHtml(item.id)}"
                   data-size="${item.size || 0}"
                   ${item.recommended ? 'checked' : ''}>
            <span class="selector-checkbox"></span>
            <span class="selector-content">
              <span class="selector-label">${escapeHtml(item.label)} ${recommended}</span>
              <span class="selector-meta">
                ${sizeInfo}
                ${stars}
                ${ramInfo}
              </span>
              ${item.description ? `<span class="selector-desc">${escapeHtml(item.description)}</span>` : ''}
            </span>
          </label>`;
      });

      html += `</div></div>`;
    }

    html += '</div>';
    return html;
  }

  /**
   * Renders quality stars (1-5).
   */
  function renderQualityStars(quality) {
    if (!quality || quality < 1 || quality > 5) return '';
    const filled = '★'.repeat(quality);
    const empty = '☆'.repeat(5 - quality);
    return `<span class="selector-stars">${filled}${empty}</span>`;
  }

  /**
   * Renders CLI command box with copy button.
   */
  function renderCLICommandBox(command, fullCommand) {
    const displayCmd = fullCommand || command || 'hfdownloader download <repo>';

    return `
      <div class="cli-command-box">
        <div class="cli-command-header">
          <span class="cli-command-label">CLI Command</span>
          <button class="btn btn-ghost btn-sm" onclick="copyCliCommand()" title="Copy to clipboard">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" width="16" height="16">
              <rect x="9" y="9" width="13" height="13" rx="2" ry="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/>
            </svg>
          </button>
        </div>
        <code class="cli-command-text" id="cliCommandText">${escapeHtml(displayCmd)}</code>
      </div>
    `;
  }

  /**
   * Renders related downloads section (e.g., base model for LoRA).
   */
  function renderRelatedDownloads(downloads) {
    if (!downloads || downloads.length === 0) return '';

    let html = `
      <div class="analysis-section related-downloads-section">
        <h4>Related Downloads</h4>
        <div class="related-downloads">`;

    downloads.forEach(dl => {
      const requiredBadge = dl.required ? '<span class="badge-required">Required</span>' : '';

      html += `
        <div class="related-download-card ${dl.required ? 'required' : ''}">
          <div class="related-download-info">
            <span class="related-download-label">${escapeHtml(dl.label)} ${requiredBadge}</span>
            <span class="related-download-repo">${escapeHtml(dl.repo)}</span>
            ${dl.description ? `<span class="related-download-desc">${escapeHtml(dl.description)}</span>` : ''}
            ${dl.size_human ? `<span class="related-download-size">${escapeHtml(dl.size_human)}</span>` : ''}
          </div>
          <button class="btn btn-secondary btn-sm" onclick="analyzeRelatedRepo('${escapeHtml(dl.repo)}')">
            Analyze
          </button>
        </div>`;
    });

    html += '</div></div>';
    return html;
  }

  /**
   * Analyze a related repo (from LoRA base model link).
   */
  window.analyzeRelatedRepo = function(repo) {
    const input = $('#analyzeInput');
    if (input) input.value = repo;
    analyzeRepo();
  };

  /**
   * Copy CLI command to clipboard.
   */
  window.copyCliCommand = function() {
    const cmdEl = $('#cliCommandText');
    if (cmdEl) {
      navigator.clipboard.writeText(cmdEl.textContent);
      showToast('Command copied to clipboard', 'success');
    }
  };

  /**
   * Update CLI command and file list based on selections.
   */
  function updateCLICommandFromSelections() {
    if (!currentAnalysis) return;

    const selectedItems = Array.from(document.querySelectorAll('.selectable-items input[type="checkbox"]:checked'))
      .map(cb => cb.value);

    let cmd = `hfdownloader download ${currentAnalysis.repo}`;

    if (currentAnalysis.is_dataset) {
      cmd += ' --dataset';
    }

    if (currentAnalysis.branch && currentAnalysis.branch !== 'main') {
      cmd += ` -b ${currentAnalysis.branch}`;
    }

    // Add filter if selections differ from "all selected" or "recommended"
    const totalItems = document.querySelectorAll('.selectable-items input[type="checkbox"]').length;
    if (selectedItems.length > 0 && selectedItems.length < totalItems) {
      cmd += ` -F ${selectedItems.join(',')}`;
    }

    // Add advanced options if set
    if (advancedOptions.exclude) {
      cmd += ` -e "${advancedOptions.exclude}"`;
    }

    const cmdEl = $('#cliCommandText');
    if (cmdEl) cmdEl.textContent = cmd;

    // Also update the legacy download command display
    const legacyCmd = $('#downloadCommand');
    if (legacyCmd) legacyCmd.textContent = cmd;

    // Update file list based on selections
    updateFileListFromSelections(selectedItems);
  }

  /**
   * Update the displayed file list based on selected items.
   */
  function updateFileListFromSelections(selectedFilters) {
    if (!currentAnalysis || !currentAnalysis.files) return;

    const filesContainer = $('#analysisFilesList');
    const countEl = $('#selectedFilesCount');
    if (!filesContainer) return;

    let filteredFiles = currentAnalysis.files;
    let selectedSize = 0;

    // If we have selectable items and some are selected, filter the files
    if (currentAnalysis.selectable_items && currentAnalysis.selectable_items.length > 0 && selectedFilters.length > 0) {
      // Build a set of filter patterns
      const filterPatterns = new Set(selectedFilters.map(f => f.toLowerCase()));

      filteredFiles = currentAnalysis.files.filter(file => {
        const filePath = (file.path || file.name || '').toLowerCase();

        // Check if file matches any of the selected filters
        for (const pattern of filterPatterns) {
          // Match various patterns: exact name, contains, extension
          if (filePath.includes(pattern) ||
              filePath.endsWith('.' + pattern) ||
              filePath.includes('/' + pattern + '/') ||
              filePath.includes('_' + pattern + '.') ||
              filePath.includes('-' + pattern + '.') ||
              filePath.includes('.' + pattern + '.')) {
            return true;
          }
        }

        // Also include config/metadata files that are always needed
        const alwaysInclude = ['config.json', 'tokenizer', '.txt', 'readme', '.md', 'generation_config'];
        for (const inc of alwaysInclude) {
          if (filePath.includes(inc)) return true;
        }

        return false;
      });

      // Calculate selected size
      selectedSize = filteredFiles.reduce((sum, f) => sum + (f.size || 0), 0);
    } else if (selectedFilters.length === 0) {
      // Nothing selected - show message
      filesContainer.innerHTML = `<div class="analysis-file" style="justify-content: center; color: var(--color-text-muted);">
        Select items above to see matching files
      </div>`;
      if (countEl) countEl.textContent = '(0 selected)';
      return;
    } else {
      // All files
      selectedSize = currentAnalysis.files.reduce((sum, f) => sum + (f.size || 0), 0);
    }

    // Render the filtered file list
    const filesHtml = filteredFiles.slice(0, 20).map(f => `
      <div class="analysis-file">
        <span class="analysis-file-name">${escapeHtml(f.path || f.name)}</span>
        <span class="analysis-file-size">${f.size_human || formatBytes(f.size)}</span>
      </div>
    `).join('');

    const moreFiles = filteredFiles.length > 20
      ? `<div class="analysis-file" style="justify-content: center; color: var(--color-text-muted);">
           ... and ${filteredFiles.length - 20} more files
         </div>`
      : '';

    filesContainer.innerHTML = filesHtml + moreFiles;

    // Update the count display
    if (countEl) {
      const sizeHuman = formatBytes(selectedSize);
      countEl.textContent = `(${filteredFiles.length} files, ${sizeHuman})`;
    }
  }

  /**
   * Initialize selectable items event handlers.
   */
  function initSelectableItems() {
    document.querySelectorAll('.selectable-items input[type="checkbox"]').forEach(cb => {
      cb.addEventListener('change', updateCLICommandFromSelections);
    });
  }

  // =========================================
  // Utilities
  // =========================================

  function escapeHtml(str) {
    if (!str) return '';
    const div = document.createElement('div');
    div.textContent = str;
    return div.innerHTML;
  }

  function formatBytes(bytes) {
    if (!bytes || bytes === 0) return '0 B';
    const k = 1024;
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB'];
    const i = Math.floor(Math.log(bytes) / Math.log(k));
    return parseFloat((bytes / Math.pow(k, i)).toFixed(1)) + ' ' + sizes[i];
  }

  // =========================================
  // Initialize
  // =========================================

  function init() {
    initNavigation();
    initWebSocket();
    initAnalyzePage();
    initDownloadPage();
    initCachePage();
    initSettingsPage();
    initMirrorPage();
    initModal();

    // Load initial data
    loadJobs();
  }

  // Start
  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', init);
  } else {
    init();
  }

})();
