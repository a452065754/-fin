(function () {
  'use strict';

  var PALETTE = ['#5470c6', '#91cc75', '#fac858', '#ee6666', '#73c0de', '#3ba272', '#fc8452', '#9a60b4', '#ea7ccc', '#48b8d0'];
  var FONT_STACK = '"PingFang SC","Microsoft YaHei","Segoe UI",Arial,sans-serif';

  var state = {
    templates: [],
    query: '',
    kind: 'all',
    preview: null,
    path: null,
    page: 0,
    fit: true,
    zoom: 1,
    scale: 1,
    pageW: 0,
    pageH: 0,
    params: {},
    charts: [],
    reqToken: 0,
    emptyHTML: ''
  };

  var els = {};
  var toastTimer = null;
  var resizeTimer = null;

  init();

  function init() {
    cacheEls();
    state.emptyHTML = els.emptyState.innerHTML;
    bindEvents();
    loadTemplates();
    if (localStorage.getItem('rv.sidebar') === '1') {
      document.getElementById('app').classList.add('sidebar-collapsed');
    }
    // On a phone the template tree would cover the report, so it starts
    // collapsed; the toolbar button brings it back.
    if (window.innerWidth <= 900) {
      els.app.classList.add('sidebar-collapsed');
    }
    var initial = readHash();
    if (initial) {
      // The hash carries the template path and its parameters, so a preview
      // link can be shared as is.
      loadPreview(initial.path, initial.params || {});
    }
  }

  function cacheEls() {
    [
      'search', 'filters', 'templateList', 'tplCount', 'docTitle', 'docPath',
      'kindBadge', 'paramBar', 'reloadBtn', 'zoomOut', 'zoomIn', 'zoomLabel', 'zoomFit',
      'warnBtn', 'warnCount', 'pageTabs', 'stage', 'emptyState', 'loading', 'previewRoot',
      'warnPanel', 'warnList', 'warnClose', 'toggleSidebar', 'app',
      'connBtn', 'connCount', 'connMask', 'connClose', 'connList', 'connHint',
      'shareBtn', 'exportBtn', 'exportMenu',
      'perfBtn', 'perfMask', 'perfClose', 'perfHint', 'perfBody'
    ].forEach(function (id) {
      els[id] = document.getElementById(id);
    });
  }

  function bindEvents() {
    els.search.addEventListener('input', function () {
      state.query = els.search.value;
      renderList();
    });

    els.filters.addEventListener('click', function (e) {
      var btn = e.target.closest('.filter');
      if (!btn) return;
      state.kind = btn.dataset.kind;
      Array.prototype.forEach.call(els.filters.children, function (b) {
        b.classList.toggle('active', b === btn);
      });
      renderList();
    });

    els.templateList.addEventListener('click', function (e) {
      var btn = e.target.closest('.tpl');
      if (!btn) return;
      openTemplate(btn.dataset.path);
    });

    els.connBtn.addEventListener('click', showConnections);
    els.connClose.addEventListener('click', function () {
      els.connMask.hidden = true;
    });
    els.connMask.addEventListener('click', function (e) {
      if (e.target === els.connMask) els.connMask.hidden = true;
    });
    els.perfBtn.addEventListener('click', showPerf);
    els.perfClose.addEventListener('click', function () {
      els.perfMask.hidden = true;
    });
    els.perfMask.addEventListener('click', function (e) {
      if (e.target === els.perfMask) els.perfMask.hidden = true;
    });

    els.reloadBtn.addEventListener('click', applyParams);
    els.shareBtn.addEventListener('click', copyShareLink);

    els.exportBtn.addEventListener('click', function (e) {
      e.stopPropagation();
      els.exportMenu.hidden = !els.exportMenu.hidden;
    });
    els.exportMenu.addEventListener('click', function (e) {
      var btn = e.target.closest('button[data-export]');
      if (!btn) return;
      els.exportMenu.hidden = true;
      exportReport(btn.dataset.export);
    });
    // Clicking anywhere else closes the menu.
    document.addEventListener('click', function () {
      els.exportMenu.hidden = true;
    });
    els.zoomIn.addEventListener('click', function () {
      state.fit = false;
      state.zoom = Math.min(3, Math.round(((state.scale || 1) + 0.1) * 10) / 10);
      applyZoom();
    });
    els.zoomOut.addEventListener('click', function () {
      state.fit = false;
      state.zoom = Math.max(0.1, Math.round(((state.scale || 1) - 0.1) * 10) / 10);
      applyZoom();
    });
    els.zoomFit.addEventListener('click', function () {
      state.fit = true;
      applyZoom();
    });

    els.warnBtn.addEventListener('click', function () {
      els.warnPanel.hidden = !els.warnPanel.hidden;
    });
    els.warnClose.addEventListener('click', function () {
      els.warnPanel.hidden = true;
    });

    els.toggleSidebar.addEventListener('click', function () {
      var collapsed = els.app.classList.toggle('sidebar-collapsed');
      localStorage.setItem('rv.sidebar', collapsed ? '1' : '0');
      if (state.fit) setTimeout(applyZoom, 240);
    });

    document.addEventListener('keydown', function (e) {
      var tag = (e.target && e.target.tagName) || '';
      if (e.key === '/' && tag !== 'INPUT' && tag !== 'TEXTAREA') {
        e.preventDefault();
        els.search.focus();
      } else if (e.key === 'Escape') {
        els.warnPanel.hidden = true;
        if (document.activeElement === els.search) els.search.blur();
      }
    });

    window.addEventListener('resize', function () {
      clearTimeout(resizeTimer);
      resizeTimer = setTimeout(function () {
        if (state.fit) applyZoom();
        resizeCharts();
      }, 120);
    });
  }

  // ---------- template list ----------

  function loadTemplates() {
    fetch('/api/templates')
      .then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
          return data;
        });
      })
      .then(function (data) {
        state.templates = data.templates || [];
        els.tplCount.textContent = state.templates.length + ' 个模板';
        renderList();
      })
      .catch(function (err) {
        els.templateList.innerHTML = '';
        var box = document.createElement('div');
        box.className = 'list-empty';
        box.textContent = '无法读取模板列表：' + err.message;
        els.templateList.appendChild(box);
        toast('无法读取模板列表：' + err.message);
      });
  }

  // Connections declared in WEB-INF/embed/finedb. Passwords live in the
  // environment, so the panel only reports which ones are ready to use.
  // Shows the per-template rendering statistics so the slow reports can be
  // spotted. The numbers come from the server, which times every render.
  function showPerf() {
    els.perfMask.hidden = false;
    els.perfHint.textContent = '正在读取…';
    els.perfBody.innerHTML = '';
    fetch('/api/perf?limit=100')
      .then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
          return data;
        });
      })
      .then(function (data) {
        var list = data.templates || [];
        if (!list.length) {
          els.perfHint.textContent = '还没有渲染记录。打开几个报表后再回来看看。';
          return;
        }
        var totalMs = 0;
        var totalCount = 0;
        list.forEach(function (t) { totalMs += t.totalMs; totalCount += t.count; });
        els.perfHint.textContent =
          '共跟踪 ' + data.tracked + ' 个模板，累计渲染 ' + totalCount +
          ' 次、耗时 ' + Math.round(totalMs) + ' 毫秒。按累计耗时排序，最慢的在前。';

        list.forEach(function (t) {
          var tr = document.createElement('tr');
          var cells = [
            { text: t.path, title: t.path, cls: 'perf-path' },
            { text: String(t.count) },
            { text: t.avgMs.toFixed(0) + ' ms' },
            { text: t.maxMs + ' ms' },
            { text: Math.round(t.hitRate * 100) + '%' },
            { text: t.errors ? String(t.errors) : '' }
          ];
          cells.forEach(function (c) {
            var td = document.createElement('td');
            td.textContent = c.text;
            if (c.title) td.title = c.title;
            if (c.cls) td.className = c.cls;
            tr.appendChild(td);
          });
          // Clicking a row opens that report.
          tr.classList.add('perf-row');
          tr.addEventListener('click', function () {
            els.perfMask.hidden = true;
            openTemplate(t.path);
          });
          els.perfBody.appendChild(tr);
        });
      })
      .catch(function (err) {
        els.perfHint.textContent = '读取失败：' + (err.message || err);
      });
  }

  function showConnections() {
    els.connMask.hidden = false;
    els.connList.innerHTML = '';
    els.connHint.textContent = '正在读取…';
    fetch('/api/connections')
      .then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
          return data;
        });
      })
      .then(function (data) {
        var list = data.connections || [];
        els.connCount.textContent = data.ready + '/' + data.total;
        var missing = list.filter(function (c) {
          return c.needsPassword && !c.resolved;
        });
        els.connHint.textContent = missing.length
          ? ('共 ' + data.total + ' 个连接，' + data.ready + ' 个可用。缺少密码的连接需设置对应环境变量后重启本程序。')
          : ('共 ' + data.total + ' 个连接，全部可用。');
        list.forEach(function (c) {
          var li = document.createElement('li');
          li.className = 'conn-item';

          var name = document.createElement('div');
          name.className = 'conn-name';
          name.textContent = c.name;

          var meta = document.createElement('div');
          meta.className = 'conn-meta';
          meta.textContent = c.kind + (c.username ? ' · ' + c.username : '') + ' · ' + c.url;

          var tag = document.createElement('span');
          var ready = !c.needsPassword || c.resolved;
          tag.className = 'conn-tag ' + (ready ? 'ok' : 'off');
          tag.textContent = ready ? '可用' : '缺密码';

          name.appendChild(tag);
          li.appendChild(name);
          li.appendChild(meta);

          if (!ready) {
            var hint = document.createElement('code');
            hint.className = 'conn-env';
            hint.textContent = c.passwordEnv;
            li.appendChild(hint);
          }
          els.connList.appendChild(li);
        });
      })
      .catch(function (err) {
        els.connHint.textContent = '读取失败：' + err.message;
      });
  }

  function renderList() {
    var q = state.query.trim().toLowerCase();
    var items = state.templates.filter(function (t) {
      if (state.kind !== 'all' && t.kind !== state.kind) return false;
      if (!q) return true;
      return (t.name + ' ' + t.path).toLowerCase().indexOf(q) >= 0;
    });

    els.templateList.innerHTML = '';
    if (!items.length) {
      var empty = document.createElement('div');
      empty.className = 'list-empty';
      empty.textContent = '没有匹配的模板';
      els.templateList.appendChild(empty);
      return;
    }

    var groups = new Map();
    items.forEach(function (t) {
      var dir = t.dir || '';
      if (!groups.has(dir)) groups.set(dir, []);
      groups.get(dir).push(t);
    });

    Array.from(groups.keys()).sort(function (a, b) {
      return a.localeCompare(b, 'zh-CN');
    }).forEach(function (dir) {
      var group = document.createElement('div');
      group.className = 'tpl-group';

      var title = document.createElement('div');
      title.className = 'tpl-group-title';
      title.textContent = dir || '根目录';
      title.title = dir || '根目录';
      group.appendChild(title);

      groups.get(dir)
        .sort(function (a, b) { return a.name.localeCompare(b.name, 'zh-CN'); })
        .forEach(function (t) { group.appendChild(templateButton(t)); });

      els.templateList.appendChild(group);
    });
  }

  function templateButton(t) {
    var btn = document.createElement('button');
    btn.className = 'tpl' + (t.path === state.path ? ' active' : '');
    btn.dataset.path = t.path;
    btn.title = t.path;

    var dot = document.createElement('span');
    dot.className = 'tpl-dot ' + (t.kind === 'fvs' ? 'fvs' : 'cpt');
    btn.appendChild(dot);

    var main = document.createElement('span');
    main.className = 'tpl-main';
    var name = document.createElement('span');
    name.className = 'tpl-name';
    name.textContent = t.name || basename(t.path);
    var meta = document.createElement('span');
    meta.className = 'tpl-dir';
    meta.textContent = t.kind.toUpperCase() + ' · ' + formatSize(t.size);
    main.appendChild(name);
    main.appendChild(meta);
    btn.appendChild(main);

    var kind = document.createElement('span');
    kind.className = 'tpl-kind';
    kind.textContent = t.kind.toUpperCase();
    btn.appendChild(kind);
    return btn;
  }

  function highlightActive() {
    Array.prototype.forEach.call(els.templateList.querySelectorAll('.tpl'), function (btn) {
      var active = btn.dataset.path === state.path;
      btn.classList.toggle('active', active);
      if (active && btn.scrollIntoView) btn.scrollIntoView({ block: 'nearest' });
    });
  }

  // ---------- preview loading ----------

  function openTemplate(path) {
    if (!path) return;
    if (state.path !== path) {
      // Restore the parameters used the last time this template was opened.
      state.params = recallParams(path) || {};
    }
    els.warnPanel.hidden = true;
    loadPreview(path, state.params);
  }

  function loadPreview(path, params) {
    var token = ++state.reqToken;
    state.params = params || {};
    state.path = path;
    syncHash();
    showLoading(true);
    els.emptyState.hidden = true;
    els.previewRoot.hidden = true;
    els.reloadBtn.disabled = true;
    els.pageTabs.hidden = true;
    hideWarnings();

    var qs = new URLSearchParams();
    qs.set('path', path);
    Object.keys(state.params).forEach(function (k) {
      var v = state.params[k];
      if (v !== undefined && v !== null && v !== '') qs.set(k, v);
    });

    fetch('/api/preview?' + qs.toString())
      .then(function (res) {
        return res.json().then(function (data) {
          if (!res.ok) throw new Error(data.error || ('HTTP ' + res.status));
          if (data.error) throw new Error(data.error);
          return data;
        });
      })
      .then(function (data) {
        if (token !== state.reqToken) return;
        state.preview = data;
        state.page = 0;
        els.emptyState.innerHTML = state.emptyHTML;
        applyMeta(data);
        renderParams(data);
        renderWarnings(data);
        renderPageTabs();
        renderPage();
        els.emptyState.hidden = true;
        els.previewRoot.hidden = false;
        els.reloadBtn.disabled = false;
        els.shareBtn.disabled = false;
        els.exportBtn.disabled = false;
        highlightActive();
        // The address bar keeps the path and the parameters, so the link can
        // be copied and shared as is.
        syncHash();
      })
      .catch(function (err) {
        if (token !== state.reqToken) return;
        state.preview = null;
        showError(path, err.message || String(err));
      })
      .finally(function () {
        if (token === state.reqToken) showLoading(false);
      });
  }

  function applyMeta(data) {
    var title = data.title || data.name || basename(data.path || state.path);
    els.docTitle.textContent = title;
    els.docPath.textContent = data.path || state.path || '';
    els.kindBadge.hidden = false;
    els.kindBadge.textContent = (data.kind || '').toUpperCase();
    els.kindBadge.classList.toggle('fvs', data.kind === 'fvs');
    document.title = title + ' · 报表查看器';
  }

  function showError(path, message) {
    els.docTitle.textContent = '无法打开模板';
    els.docPath.textContent = path || '';
    els.kindBadge.hidden = true;
    els.previewRoot.innerHTML = '';
    els.previewRoot.hidden = true;
    els.pageTabs.hidden = true;
    els.emptyState.hidden = false;
    var h2 = els.emptyState.querySelector('h2');
    var p = els.emptyState.querySelector('p');
    if (h2) h2.textContent = '模板渲染失败';
    if (p) p.textContent = message;
    toast('渲染失败：' + message);
    showLoading(false);
  }

  function showLoading(on) {
    els.loading.hidden = !on;
    if (on) {
      els.emptyState.hidden = true;
      els.previewRoot.hidden = true;
    }
  }

  // Remembers the parameters used for a template, so reopening it restores
  // the last view instead of the bare defaults.
  function rememberParams(path, params) {
    if (!path) return;
    try {
      var saved = JSON.parse(localStorage.getItem('rv.params') || '{}');
      saved[path] = params || {};
      localStorage.setItem('rv.params', JSON.stringify(saved));
    } catch (e) { /* storage may be unavailable */ }
  }

  // Reads back the remembered parameters of a template.
  function recallParams(path) {
    try {
      var saved = JSON.parse(localStorage.getItem('rv.params') || '{}');
      return saved[path] || null;
    } catch (e) {
      return null;
    }
  }

  function renderParams(data) {
    els.paramBar.innerHTML = '';
    var params = data.params || [];
    if (!params.length) return;

    params.forEach(function (p) {
      var label = document.createElement('label');
      label.className = 'param-field';
      var name = document.createElement('span');
      name.textContent = p.name;
      var input = document.createElement('input');
      input.type = 'text';
      input.dataset.param = p.name;
      input.placeholder = p.default || '';
      var saved = state.params[p.name];
      input.value = saved !== undefined && saved !== '' ? saved : (p.default || '');
      input.addEventListener('keydown', function (e) {
        if (e.key === 'Enter') applyParams();
      });
      label.appendChild(name);
      label.appendChild(input);
      els.paramBar.appendChild(label);
    });

    var apply = document.createElement('button');
    apply.className = 'btn primary';
    apply.textContent = '查询';
    apply.addEventListener('click', applyParams);
    els.paramBar.appendChild(apply);
  }

  // Copies the current preview link to the clipboard. The link carries the
  // template path and the parameters, so the recipient sees the same report.
  function copyShareLink() {
    if (!state.path) return;
    syncHash();
    var url = location.href;
    var done = function (ok) {
      toast(ok ? '链接已复制，可直接发给同事' : '复制失败，请手动复制地址栏');
    };
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(url).then(function () { done(true); }, function () {
        done(fallbackCopy(url));
      });
      return;
    }
    done(fallbackCopy(url));
  }

  // Older browsers and non-secure origins have no clipboard API.
  function fallbackCopy(text) {
    try {
      var ta = document.createElement('textarea');
      ta.value = text;
      ta.setAttribute('readonly', '');
      ta.style.position = 'fixed';
      ta.style.left = '-9999px';
      document.body.appendChild(ta);
      ta.select();
      var ok = document.execCommand('copy');
      document.body.removeChild(ta);
      return ok;
    } catch (e) {
      return false;
    }
  }

  // ---------- export ----------

  // Exports the current report. PNG captures the rendered canvas (charts are
  // drawn on canvas, so the browser can hand over the pixels directly), while
  // PDF goes through the browser's own print pipeline, which keeps text
  // selectable and handles tables of any size.
  function exportReport(kind) {
    if (!state.path) return;
    if (kind === 'pdf') {
      exportPdf();
      return;
    }
    exportPng();
  }

  function exportPng() {
    var root = els.previewRoot;
    var canvases = root ? root.querySelectorAll('canvas') : [];
    if (!canvases.length) {
      toast('该报表没有可导出的图表（表格报表请用「打印 / 另存为 PDF」）');
      return;
    }
    try {
      // Stack every chart into one image so the export is a single file.
      var ratio = 2;
      var gap = 12;
      var width = 0;
      var height = 0;
      var total = 0;
      Array.prototype.forEach.call(canvases, function (c) {
        width = Math.max(width, c.width);
        height += c.height;
        total++;
      });
      height += gap * (total - 1);

      var out = document.createElement('canvas');
      out.width = width;
      out.height = height;
      var ctx = out.getContext('2d');
      ctx.fillStyle = '#ffffff';
      ctx.fillRect(0, 0, width, height);
      var y = 0;
      Array.prototype.forEach.call(canvases, function (c) {
        ctx.drawImage(c, 0, y);
        y += c.height + gap;
      });
      var url = out.toDataURL('image/png');
      downloadDataUrl(url, exportFileName() + '.png');
      toast('已导出 ' + total + ' 个图表');
    } catch (e) {
      toast('导出失败：' + (e && e.message ? e.message : e));
    }
  }

  function exportPdf() {
    // The print stylesheet hides the chrome and lays the report out on one
    // page, so the browser's "save as PDF" produces a clean document.
    document.body.classList.add('exporting');
    var restore = function () {
      document.body.classList.remove('exporting');
      window.removeEventListener('afterprint', restore);
    };
    window.addEventListener('afterprint', restore);
    setTimeout(function () {
      window.print();
      // Some browsers do not fire afterprint.
      setTimeout(restore, 1500);
    }, 50);
  }

  function downloadDataUrl(dataUrl, filename) {
    var a = document.createElement('a');
    a.href = dataUrl;
    a.download = filename;
    document.body.appendChild(a);
    a.click();
    document.body.removeChild(a);
  }

  // Builds a safe file name from the report title.
  function exportFileName() {
    var name = (state.preview && (state.preview.title || state.preview.name)) ||
               basename(state.path) || '报表';
    return String(name).replace(/[\\/:*?"<>|]+/g, '_').slice(0, 80);
  }

  function applyParams() {
    if (!state.path) return;
    var params = {};
    Array.prototype.forEach.call(els.paramBar.querySelectorAll('input[data-param]'), function (input) {
      params[input.dataset.param] = input.value;
    });
    rememberParams(state.path, params);
    loadPreview(state.path, params);
  }

  function renderWarnings(data) {
    var warnings = data.warnings || [];
    els.warnBtn.hidden = warnings.length === 0;
    els.warnCount.textContent = warnings.length;
    els.warnList.innerHTML = '';
    warnings.forEach(function (w) {
      var li = document.createElement('li');
      li.textContent = w;
      els.warnList.appendChild(li);
    });
    if (!warnings.length) els.warnPanel.hidden = true;
  }

  function hideWarnings() {
    els.warnBtn.hidden = true;
    els.warnCount.textContent = '0';
    els.warnList.innerHTML = '';
    els.warnPanel.hidden = true;
  }

  function renderPageTabs() {
    var data = state.preview;
    if (!data) return;
    var pages = data.kind === 'fvs' ? (data.pages || []) : (data.sheets || []);
    els.pageTabs.innerHTML = '';
    if (pages.length <= 1) {
      els.pageTabs.hidden = true;
      return;
    }
    els.pageTabs.hidden = false;
    pages.forEach(function (p, i) {
      var btn = document.createElement('button');
      btn.className = 'page-tab' + (i === state.page ? ' active' : '');
      btn.textContent = p.name || (data.kind === 'fvs' ? '第 ' + (i + 1) + ' 屏' : 'Sheet' + (i + 1));
      btn.addEventListener('click', function () {
        if (state.page === i) return;
        state.page = i;
        Array.prototype.forEach.call(els.pageTabs.children, function (b, j) {
          b.classList.toggle('active', j === i);
        });
        renderPage();
        els.stage.scrollTop = 0;
        els.stage.scrollLeft = 0;
      });
      els.pageTabs.appendChild(btn);
    });
  }

  // ---------- page rendering ----------

  function renderPage() {
    disposeCharts();
    els.previewRoot.innerHTML = '';
    var data = state.preview;
    if (!data) return;
    if (data.kind === 'fvs') {
      renderFVS(data);
    } else {
      renderCPT(data);
    }
    applyZoom();
    requestAnimationFrame(function () {
      initCharts(els.previewRoot);
    });
  }

  function renderCPT(data) {
    var sheet = (data.sheets || [])[state.page];
    if (!sheet) {
      toast('该报表没有可显示的页面');
      return;
    }
    var colX = prefixSums(sheet.cols || []);
    var rowY = prefixSums(sheet.rows || []);
    var width = sheet.width || colX[colX.length - 1] || 800;
    var height = sheet.height || rowY[rowY.length - 1] || 600;
    state.pageW = width;
    state.pageH = height;

    var wrap = document.createElement('div');
    wrap.className = 'doc-wrap';
    var box = document.createElement('div');
    box.className = 'page-box';
    var scaleEl = document.createElement('div');
    scaleEl.className = 'page-scale';
    scaleEl.style.width = width + 'px';
    scaleEl.style.height = height + 'px';

    var sheetEl = document.createElement('div');
    sheetEl.className = 'sheet';
    sheetEl.style.width = width + 'px';
    sheetEl.style.height = height + 'px';
    sheetEl.appendChild(buildSheetBody(sheet, colX, rowY));

    scaleEl.appendChild(sheetEl);
    box.appendChild(scaleEl);
    wrap.appendChild(box);
    els.previewRoot.appendChild(wrap);
    // An H5 template was authored for the phone client, so the preview
    // switches to the mobile layout.
    if (data.h5) {
      applyH5Mode(data.h5, width);
    }
  }

  // applyH5Mode switches the preview into the phone layout. The H5 marker
  // tells the viewer which pans the mobile canvas allows and whether
  // pinch-zoom is permitted.
  function applyH5Mode(h5, width) {
    var root = els.previewRoot;
    root.classList.add('h5-mode');
    if (h5.horizontal === false) root.classList.add('h5-noscroll-x');
    if (h5.vertical === false) root.classList.add('h5-noscroll-y');
    if (!h5.zoom) root.classList.add('h5-nozoom');
    if (h5.canvasSize) root.classList.add('h5-canvassize');
    // The phone canvas is as wide as the screen, so the sheet is scaled
    // down to fit instead of scrolling sideways.
    var avail = Math.max(240, els.stage.clientWidth - 24);
    var scale = Math.min(1, avail / Math.max(1, width));
    if (scale < 0.35) scale = 0.35;
    state.fit = false;
    state.zoom = scale;
    applyZoom();
    // The template tree would cover the phone-sized report.
    if (els.app) els.app.classList.add('sidebar-collapsed');
    var badge = els.kindBadge;
    if (badge && badge.textContent.indexOf('H5') < 0) {
      badge.textContent = badge.textContent + ' · H5';
    }
  }

  function renderFVS(data) {
    var page = (data.pages || [])[state.page];
    if (!page) {
      toast('该大屏没有可显示的页面');
      return;
    }
    var width = page.width || 1920;
    var height = page.height || 1080;
    state.pageW = width;
    state.pageH = height;

    var wrap = document.createElement('div');
    wrap.className = 'doc-wrap';
    var box = document.createElement('div');
    box.className = 'page-box';
    var scaleEl = document.createElement('div');
    scaleEl.className = 'page-scale fvs-page';
    scaleEl.style.width = width + 'px';
    scaleEl.style.height = height + 'px';
    scaleEl.style.background = page.background || '#ffffff';
    if (page.backgroundImage) {
      scaleEl.style.backgroundImage = 'url("' + page.backgroundImage.replace(/"/g, '%22') + '")';
      scaleEl.style.backgroundSize = page.backgroundSize || '100% 100%';
      scaleEl.style.backgroundPosition = 'center center';
      scaleEl.style.backgroundRepeat = (page.backgroundSize === 'auto') ? 'repeat' : 'no-repeat';
    }

    (page.widgets || []).forEach(function (w) {
      var el = buildWidget(w, scaleEl);
      if (el) scaleEl.appendChild(el);
    });

    box.appendChild(scaleEl);
    wrap.appendChild(box);
    els.previewRoot.appendChild(wrap);
  }

  function buildSheet(sheet) {
    var colX = prefixSums(sheet.cols || []);
    var rowY = prefixSums(sheet.rows || []);
    var width = sheet.width || colX[colX.length - 1] || 300;
    var height = sheet.height || rowY[rowY.length - 1] || 200;
    var el = document.createElement('div');
    el.className = 'sheet';
    el.style.width = width + 'px';
    el.style.height = height + 'px';
    el.appendChild(buildSheetBody(sheet, colX, rowY));
    return el;
  }

  function buildSheetBody(sheet, colX, rowY) {
    var frag = document.createDocumentFragment();
    (sheet.cells || []).forEach(function (cell) {
      var r = Number(cell.r) || 0;
      var c = Number(cell.c) || 0;
      var rs = Math.max(1, Number(cell.rs) || 1);
      var cs = Math.max(1, Number(cell.cs) || 1);
      var x = colX[c] !== undefined ? colX[c] : 0;
      var y = rowY[r] !== undefined ? rowY[r] : 0;
      var x2 = colX[c + cs] !== undefined ? colX[c + cs] : x;
      var y2 = rowY[r + rs] !== undefined ? rowY[r + rs] : y;
      var w = x2 - x;
      var h = y2 - y;
      if (w <= 0 || h <= 0) return;

      var el = document.createElement('div');
      el.className = 'cell';
      el.style.left = x + 'px';
      el.style.top = y + 'px';
      el.style.width = w + 'px';
      el.style.height = h + 'px';
      applyCellStyle(el, cell.style || {});

      if (cell.chart) {
        el.classList.add('cell-chart');
        el.appendChild(chartBox(cell.chart));
      } else if (cell.widget) {
        el.classList.add('cell-widget');
        el.appendChild(buildFormControl(cell));
      } else if (cell.text !== undefined && cell.text !== null && cell.text !== '') {
        var inner = document.createElement('div');
        inner.className = 'cell-inner';
        inner.textContent = String(cell.text);
        el.appendChild(inner);
      }
      frag.appendChild(el);
    });
    return frag;
  }

  // buildFormControl renders a fill-in control (填报控件). The value comes
  // from the cell itself; the widget only decides how it is presented.
  // The name differs from buildWidget, which builds FVS dashboard widgets.
  function buildFormControl(cell) {
    var w = cell.widget || {};
    var kind = String(w.kind || 'text').toLowerCase();
    var value = cell.text === undefined || cell.text === null ? '' : String(cell.text);
    var box = document.createElement('div');
    box.className = 'widget widget-' + kind;

    // A button-like control only carries an action, so it shows a label.
    if (kind === 'button' || kind === 'toolbar' || kind.indexOf('rowbutton') >= 0) {
      var btn = document.createElement('button');
      btn.type = 'button';
      btn.className = 'widget-btn';
      btn.textContent = value || '操作';
      btn.disabled = true;
      box.appendChild(btn);
      return box;
    }

    if (!w.editable) {
      var ro = document.createElement('div');
      ro.className = 'widget-readonly';
      ro.textContent = value;
      box.appendChild(ro);
      return box;
    }

    var input;
    if (kind === 'combo' || kind === 'multicombo' || kind === 'combocheckbox' ||
        kind === 'dropdown' || kind === 'tree') {
      input = document.createElement('select');
      input.className = 'widget-input';
      var blank = document.createElement('option');
      blank.value = '';
      blank.textContent = '';
      input.appendChild(blank);
      var opts = w.options || [];
      var inOptions = false;
      opts.forEach(function (opt) {
        var o = document.createElement('option');
        o.value = opt;
        o.textContent = opt;
        if (opt === value) {
          o.selected = true;
          inOptions = true;
        }
        input.appendChild(o);
      });
      if (value && !inOptions) {
        // The current value is not among the options; keep it visible.
        var extra = document.createElement('option');
        extra.value = value;
        extra.textContent = value;
        extra.selected = true;
        input.appendChild(extra);
      }
    } else if (kind === 'checkbox' || kind === 'checkboxgroup') {
      input = document.createElement('input');
      input.type = 'checkbox';
      input.className = 'widget-input widget-check';
      input.checked = value === 'true' || value === '1' || value === '是';
    } else if (kind === 'date') {
      input = document.createElement('input');
      input.type = 'date';
      input.className = 'widget-input';
      input.value = normalizeDate(value);
    } else if (kind === 'textarea') {
      input = document.createElement('textarea');
      input.className = 'widget-input';
      input.value = value;
    } else if (kind === 'number' || kind === 'money' || kind === 'percent') {
      input = document.createElement('input');
      input.type = 'number';
      input.className = 'widget-input';
      input.value = value.replace(/[^0-9.\-]/g, '');
    } else {
      input = document.createElement('input');
      input.type = 'text';
      input.className = 'widget-input';
      input.value = value;
      if (w.scanCode) input.setAttribute('inputmode', 'text');
    }
    input.addEventListener('click', function (ev) { ev.stopPropagation(); });
    input.addEventListener('mousedown', function (ev) { ev.stopPropagation(); });
    box.appendChild(input);
    if (w.description) box.title = w.description;
    return box;
  }

  // pad2 left-pads a number to two digits.
  function pad2(n) {
    n = String(n);
    return n.length >= 2 ? n : '0' + n;
  }

  // normalizeDate turns the report's date text into an <input type=date> value.
  function normalizeDate(v) {
    if (!v) return '';
    var m = String(v).match(/(\d{4})\s*[-/年]\s*(\d{1,2})\s*[-/月]\s*(\d{1,2})/);
    if (!m) return '';
    return m[1] + '-' + pad2(m[2]) + '-' + pad2(m[3]);
  }

  function applyCellStyle(el, s) {
    if (s.fontFamily) el.style.fontFamily = s.fontFamily;
    if (s.fontSize > 0) el.style.fontSize = s.fontSize + 'px';
    if (s.bold) el.style.fontWeight = '600';
    if (s.italic) el.style.fontStyle = 'italic';
    if (s.underline) el.style.textDecoration = 'underline';
    if (s.color) el.style.color = s.color;
    if (s.background) el.style.backgroundColor = s.background;
    if (s.hAlign) {
      el.style.textAlign = s.hAlign === 'justify' ? 'justify' : s.hAlign;
    }
    var vAlign = { top: 'flex-start', middle: 'center', bottom: 'flex-end' }[s.vAlign];
    el.style.alignItems = vAlign || 'center';
    if (s.borderTop) el.style.borderTop = s.borderTop;
    if (s.borderRight) el.style.borderRight = s.borderRight;
    if (s.borderBottom) el.style.borderBottom = s.borderBottom;
    if (s.borderLeft) el.style.borderLeft = s.borderLeft;
    if (!s.wrap) el.classList.add('nowrap');
  }

  function buildWidget(w, pageEl) {
    var el = document.createElement('div');
    el.className = 'fvs-widget';
    el.dataset.id = w.id || '';
    el.style.left = (Number(w.x) || 0) + 'px';
    el.style.top = (Number(w.y) || 0) + 'px';
    el.style.width = Math.max(0, Number(w.w) || 0) + 'px';
    el.style.height = Math.max(0, Number(w.h) || 0) + 'px';
    el.style.zIndex = String(Number(w.z) || 1);
    if (w.pane) el.dataset.pane = w.pane;
    if (w.hidden) el.style.display = 'none';

    switch (w.type) {
      case 'chart':
        el.classList.add('fvs-chart');
        el.appendChild(chartBox(w.chart));
        break;
      case 'workbook':
        renderWorkbookWidget(el, w);
        break;
      case 'image':
        el.classList.add('fvs-image-box');
        if (w.imageUrl) {
          var img = document.createElement('img');
          img.className = 'fvs-image';
          img.alt = '';
          img.draggable = false;
          img.src = w.imageUrl;
          img.addEventListener('error', function () {
            img.classList.add('fvs-image-missing');
            img.removeAttribute('src');
          });
          el.appendChild(img);
        }
        break;
      case 'slideshow':
        el.classList.add('fvs-slideshow');
        el.style.zIndex = '9000';
        if (w.tab && w.tab.tabs && w.tab.tabs.length) {
          el.appendChild(buildTabBar(w, pageEl));
        } else {
          return null;
        }
        break;
      case 'text':
      case 'text-title':
      case 'text-time':
      case 'd-text':
      case 'd-text-title':
      case 'd-text-time':
        var text = document.createElement('div');
        text.className = 'fvs-text';
        applyTextStyle(text, w.style || {});
        if (w.html) {
          text.innerHTML = w.html;
        } else {
          text.textContent = w.text || '';
        }
        el.appendChild(text);
        break;
      default:
        return null;
    }
    return el;
  }

  function renderWorkbookWidget(el, w) {
    el.classList.add('fvs-workbook');
    if (!w.sheet) {
      el.classList.add('chart-empty');
      el.textContent = '报表块无数据';
      return;
    }
    var sheet = w.sheet;
    var sheetEl = buildSheet(sheet);
    var sw = sheet.width || sheetEl.offsetWidth || 1;
    var sh = sheet.height || 1;
    var scale = Math.max(0.1, Math.min(3, (Number(w.w) || sw) / sw));
    var inner = document.createElement('div');
    inner.className = 'fvs-workbook-scale';
    inner.style.width = Math.round(sw * scale) + 'px';
    inner.style.height = Math.round(sh * scale) + 'px';
    sheetEl.style.transform = 'scale(' + scale + ')';
    inner.appendChild(sheetEl);
    el.appendChild(inner);
  }

  function buildTabBar(w, pageEl) {
    var tab = w.tab;
    var bar = document.createElement('div');
    var pos = tab.position || 'top';
    if (['top', 'bottom', 'left', 'right'].indexOf(pos) < 0) pos = 'top';
    bar.className = 'fvs-tabbar pos-' + pos;
    if (tab.activeBg) bar.style.setProperty('--tab-active-bg', tab.activeBg);
    if (tab.fontColor) bar.style.setProperty('--tab-fg', tab.fontColor);

    (tab.tabs || []).forEach(function (name, i) {
      var btn = document.createElement('button');
      btn.className = 'fvs-tab' + (i === tab.active ? ' active' : '');
      btn.textContent = name || ('页签 ' + (i + 1));
      if (tab.fontSize > 0) btn.style.fontSize = tab.fontSize + 'px';
      btn.addEventListener('click', function () {
        activateTab(pageEl, tab, bar, i);
      });
      bar.appendChild(btn);
    });
    return bar;
  }

  function activateTab(pageEl, tab, bar, idx) {
    tab.active = idx;
    Array.prototype.forEach.call(bar.children, function (btn, i) {
      btn.classList.toggle('active', i === idx);
    });
    var paneIds = tab.paneIds || [];
    var widgets = pageEl.querySelectorAll('.fvs-widget[data-pane]');
    Array.prototype.forEach.call(widgets, function (el) {
      var j = paneIds.indexOf(el.dataset.pane);
      if (j < 0) return;
      el.style.display = j === idx ? '' : 'none';
    });
    requestAnimationFrame(function () {
      initCharts(pageEl);
      resizeCharts();
    });
  }

  function applyTextStyle(el, s) {
    if (s.fontFamily) el.style.fontFamily = s.fontFamily;
    if (s.fontSize > 0) el.style.fontSize = s.fontSize + 'px';
    if (s.fontWeight) el.style.fontWeight = s.fontWeight;
    if (s.color) el.style.color = s.color;
    if (s.letterSpacing) el.style.letterSpacing = s.letterSpacing + 'px';
    if (s.lineHeight) el.style.lineHeight = s.lineHeight;
    if (s.align) {
      el.style.textAlign = s.align === 'start' ? 'left' : s.align === 'end' ? 'right' : s.align;
    }
    if (s.vAlign) {
      el.style.justifyContent = { top: 'flex-start', middle: 'center', bottom: 'flex-end' }[s.vAlign] || 'center';
    }
    if (s.writingMode) el.style.writingMode = s.writingMode;
    el.style.whiteSpace = 'pre-wrap';
  }

  // ---------- charts ----------

  function chartBox(spec) {
    var box = document.createElement('div');
    box.className = 'chart-box';
    if (!spec || !spec.series || !spec.series.length) {
      box.classList.add('chart-empty');
      box.textContent = '图表无数据';
      return box;
    }
    box._spec = spec;
    return box;
  }

  function initCharts(root) {
    if (!window.echarts) return;
    Array.prototype.forEach.call(root.querySelectorAll('.chart-box'), function (el) {
      if (el.__chart || !el._spec) return;
      if (!el.getClientRects().length) return;
      if (el.clientWidth < 6 || el.clientHeight < 6) return;
      var option = chartOption(el._spec);
      if (!option) {
        el.classList.add('chart-empty');
        el.textContent = '图表无数据';
        return;
      }
      try {
        var inst = window.echarts.init(el, null, { renderer: 'canvas' });
        inst.setOption(option, true);
        el.__chart = inst;
        state.charts.push(inst);
      } catch (err) {
        if (window.console) console.warn('chart init failed', err);
      }
    });
  }

  function disposeCharts() {
    state.charts.forEach(function (inst) {
      try { inst.dispose(); } catch (e) { /* ignore */ }
    });
    state.charts = [];
  }

  function resizeCharts() {
    state.charts.forEach(function (inst) {
      try { inst.resize(); } catch (e) { /* ignore */ }
    });
  }

  function chartOption(spec) {
    var kind = spec.kind || 'bar';
    var colors = spec.colors && spec.colors.length ? spec.colors : PALETTE;
    var series = spec.series || [];
    var cats = spec.categories || [];
    var hasTitle = !!(spec.title && String(spec.title).trim());
    var showLegend = spec.legend !== false && kind !== 'gauge' && series.length > 0;
    var legendPos = spec.legendPos || '';
    var titleSize = spec.titleSize > 0 ? spec.titleSize : 14;
    var titleBand = 4 + Math.ceil(titleSize * 1.25);
    var base = {
      color: colors,
      animationDuration: 380,
      textStyle: { fontFamily: FONT_STACK, color: '#374151' },
      title: {
        show: hasTitle,
        text: spec.title || '',
        left: 'center',
        top: 4,
        textStyle: {
          fontSize: titleSize,
          fontWeight: 600,
          color: spec.titleColor || '#1f2937',
          fontFamily: FONT_STACK
        }
      }
    };
    var legend = {
      show: showLegend,
      type: 'scroll',
      itemWidth: 10,
      itemHeight: 8,
      itemGap: 9,
      textStyle: { fontSize: 11, color: '#4b5563', fontFamily: FONT_STACK }
    };
    if (showLegend) {
      if (legendPos === 'bottom') {
        legend.bottom = 2;
        legend.left = 'center';
      } else if (legendPos === 'left') {
        legend.left = 4;
        legend.top = 'middle';
        legend.orient = 'vertical';
      } else if (legendPos === 'right') {
        legend.right = 4;
        legend.top = 'middle';
        legend.orient = 'vertical';
      } else {
        legend.top = hasTitle ? titleBand : 2;
        legend.left = 'center';
      }
    }

    if (kind === 'pie' || kind === 'funnel') {
      var slices = cats.map(function (name, i) {
        var v = 0;
        series.forEach(function (s) { v += Number(s.data[i]) || 0; });
        return { name: name, value: v };
      });
      if (!slices.length) return null;
      if (kind === 'pie') {
        return shallow(base, {
          tooltip: { trigger: 'item', formatter: '{b}<br/>{c} ({d}%)' },
          legend: legend,
          series: [{
            type: 'pie',
            radius: ['30%', '62%'],
            center: ['50%', '54%'],
            minAngle: 2,
            avoidLabelOverlap: true,
            itemStyle: { borderColor: '#fff', borderWidth: 1 },
            label: { fontSize: 10, color: '#475467', formatter: '{b}\n{d}%' },
            labelLine: { length: 8, length2: 6 },
            data: slices
          }]
        });
      }
      return shallow(base, {
        tooltip: { trigger: 'item', formatter: '{b}: {c}' },
        legend: legend,
        series: [{
          type: 'funnel',
          left: '12%',
          right: '12%',
          top: hasTitle ? 46 : 24,
          bottom: 18,
          sort: 'descending',
          gap: 2,
          minSize: '18%',
          label: { show: true, position: 'inside', fontSize: 10, color: '#fff', formatter: '{b} {c}' },
          itemStyle: { borderColor: '#fff', borderWidth: 1 },
          data: slices
        }]
      });
    }

    if (kind === 'gauge') {
      // A gauge whose dataset could not be decoded has no series at all.
      // Drawing it as zero would be a lie, so render nothing instead.
      if (!series.length || !series[0] || !series[0].data || !series[0].data.length) {
        return null;
      }
      // The backend supplies the dial scale (gaugeMin/gaugeMax) and the
      // threshold bands, so the dial is drawn the way the template declares
      // rather than guessed from the value.
      var value = Number(series[0].data[0]) || 0;
      var gMin = (typeof spec.gaugeMin === 'number') ? spec.gaugeMin : 0;
      var gMax = (typeof spec.gaugeMax === 'number' && spec.gaugeMax > gMin) ? spec.gaugeMax : 0;
      if (!gMax) {
        gMax = Math.max(Math.abs(value) * 1.25, 100);
        if (gMax < 10) gMax = 10;
      }
      var bands = spec.gaugeBands || [];
      // FineReport's band thresholds sometimes exceed the declared axis range
      // (a gauge may declare 0..100 while its bands run 0..600). Widen the
      // scale so no band is clipped away.
      bands.forEach(function (b) {
        if (b.max > gMax) gMax = b.max;
        if (b.min < gMin) gMin = b.min;
      });
      var axisColors = null;
      if (bands.length) {
        // Build the ECharts axis colour stops from the threshold bands.
        var stops = bands.map(function (b) {
          var from = ((b.min - gMin) / (gMax - gMin));
          var to = ((b.max - gMin) / (gMax - gMin));
          return [
            Math.max(0, Math.min(1, from)),
            Math.max(0, Math.min(1, to)),
            b.color || colors[0]
          ];
        }).sort(function (a, b) { return a[0] - b[0]; });
        axisColors = stops.map(function (s) { return [s[1], s[2]]; });
      }
      var target = null;
      if (spec.axisValues && spec.axisValues.length) {
        target = Number(spec.axisValues[0]);
      }
      return shallow(base, {
        tooltip: { formatter: '{b}: {c}' },
        series: [{
          type: 'gauge',
          min: gMin,
          max: gMax,
          startAngle: 210,
          endAngle: -30,
          progress: { show: true, width: 10 },
          axisLine: {
            lineStyle: {
              width: 10,
              color: axisColors || [[1, '#e8eef7']]
            }
          },
          axisTick: { show: false },
          splitLine: { show: false },
          axisLabel: { distance: 12, fontSize: 9, color: '#98a2b3' },
          pointer: { width: 4, length: '58%', itemStyle: { color: colors[0] } },
          anchor: { show: true, size: 8, itemStyle: { color: colors[0] } },
          detail: {
            valueAnimation: true,
            fontSize: 18,
            offsetCenter: [0, '60%'],
            color: '#1f2937',
            formatter: function (v) { return formatNumber(v); }
          },
          data: [{ value: value, name: (series[0] && series[0].name) || '' }]
        }]
      });
    }

    if (kind === 'radar') {
      if (cats.length < 3) return null;
      var all = [];
      series.forEach(function (s) {
        s.data.forEach(function (v) { if (isFinite(v)) all.push(Number(v)); });
      });
      var maxV = all.length ? Math.max.apply(null, all) : 100;
      maxV = Math.ceil(maxV * 1.2) || 100;
      return shallow(base, {
        tooltip: { trigger: 'item' },
        legend: legend,
        radar: {
          indicator: cats.map(function (name) { return { name: name, max: maxV }; }),
          center: ['50%', '55%'],
          radius: '62%',
          splitNumber: 4,
          axisName: { fontSize: 10, color: '#667085' },
          splitLine: { lineStyle: { color: '#e7ecf3' } },
          splitArea: { areaStyle: { color: ['#fff', '#f8fafc'] } },
          axisLine: { lineStyle: { color: '#e7ecf3' } }
        },
        series: [{
          type: 'radar',
          symbolSize: 3,
          areaStyle: { opacity: 0.12 },
          data: series.map(function (s) {
            return { name: s.name || '', value: s.data, lineStyle: { width: 2 } };
          })
        }]
      });
    }

    if (kind === 'bubble') {
      // Bubble charts carry the x value and a per-point radius alongside the y
      // value, so they render as a scatter with a symbol size array.
      var xs = spec.axisValues || [];
      var pts = [];
      series.forEach(function (s) {
        (s.data || []).forEach(function (y, i) {
          pts.push([Number(xs[i]) || i, Number(y) || 0, Number((s.size || [])[i]) || 8]);
        });
      });
      if (!pts.length) return null;
      return shallow(base, {
        tooltip: { trigger: 'item', textStyle: { fontSize: 11, fontFamily: FONT_STACK } },
        legend: legend,
        grid: { left: 10, right: 12, top: gridTop, bottom: 6, containLabel: true },
        xAxis: shallow(valAxis, { name: (spec.axisLabel || [])[0] || '', nameTextStyle: { fontSize: 10 } }),
        yAxis: shallow(valAxis, { name: (spec.axisLabel || [])[1] || '', nameTextStyle: { fontSize: 10 } }),
        series: [{
          type: 'scatter',
          data: pts,
          symbolSize: function (d) { return d[2]; },
          emphasis: { focus: 'series' }
        }]
      });
    }

    if (kind === 'combo') {
      // FineReport mixes plot kinds per series; the backend resolved each one.
      var hasY1 = series.some(function (s) { return s.axis === 'y1'; });
      var comboSeries = series.map(function (s) {
        var t = s.type || 'bar';
        var o = { name: s.name || '', type: t === 'column' ? 'bar' : t, data: s.data || [], yAxisIndex: s.axis === 'y1' ? 1 : 0 };
        if (o.type === 'line') {
          o.symbol = 'circle';
          o.symbolSize = 4;
          o.lineStyle = { width: 2 };
          if (o.yAxisIndex) o.lineStyle = { width: 2, type: 'dashed' };
        } else if (o.type === 'bar') {
          o.barMaxWidth = 28;
          o.itemStyle = { borderRadius: [2, 2, 0, 0] };
        }
        return o;
      });
      return shallow(base, {
        tooltip: { trigger: 'axis', textStyle: { fontSize: 11, fontFamily: FONT_STACK } },
        legend: legend,
        grid: {
          left: (showLegend && legendPos === 'left') ? 92 : 10,
          right: (hasY1 ? 46 : 12) + ((showLegend && legendPos === 'right') ? 76 : 0),
          top: gridTop, bottom: 6, containLabel: true
        },
        xAxis: catAxis,
        yAxis: hasY1 ? [valAxis, shallow(valAxis, { splitLine: { show: false } })] : valAxis,
        series: comboSeries
      });
    }

    if (kind === 'multipie') {
      // Multi-layer pie becomes concentric rings, one per series.
      var rings = series.map(function (s, i) {
        return {
          name: s.name || '',
          type: 'pie',
          radius: [32 + i * 13 + '%', 42 + i * 13 + '%'],
          center: ['50%', hasTitle ? '56%' : '50%'],
          label: { show: false },
          labelLine: { show: false },
          data: cats.map(function (c, j) { return { name: c, value: Number((s.data || [])[j]) || 0 }; })
        };
      }).filter(function (r) { return r.data.some(function (d) { return d.value; }); });
      if (!rings.length) return null;
      return shallow(base, {
        tooltip: { trigger: 'item', textStyle: { fontSize: 11, fontFamily: FONT_STACK } },
        legend: legend,
        series: rings
      });
    }

    if (kind === 'sunburst' || kind === 'treemap') {
      // Hierarchy: nodes arrive as "parent / child" labels.
      var treeData = (series[0] && series[0].data || []).map(function (v, i) {
        var label = String(cats[i] == null ? '' : cats[i]);
        var parts = label.split(' / ');
        var node = { name: parts[parts.length - 1], value: Number(v) || 0 };
        for (var k = parts.length - 2; k >= 0; k--) {
          node = { name: parts[k], children: [node] };
        }
        return node;
      });
      if (!treeData.length) return null;
      return shallow(base, {
        tooltip: { trigger: 'item', textStyle: { fontSize: 11, fontFamily: FONT_STACK } },
        legend: { show: false },
        series: kind === 'treemap'
          ? [{
              type: 'treeMap', data: treeData, roam: false, nodeClick: false,
              breadcrumb: { show: false },
              label: { fontSize: 10, formatter: '{b}' },
              upperLabel: { show: true, height: 16, fontSize: 10 },
              itemStyle: { borderColor: '#fff', borderWidth: 1 },
              splitArea: { show: false }
            }]
          : [{
              type: 'sunburst', data: treeData, radius: ['12%', '78%'],
              center: ['50%', hasTitle ? '56%' : '50%'],
              label: { fontSize: 10, formatter: '{b}' },
              itemStyle: { borderColor: '#fff', borderWidth: 1 }
            }]
      });
    }

    if (kind === 'heatmap') {
      // Series carry one column per row; render the first as a value grid.
      var hRows = series[0] && series[0].data || [];
      var hCats = series[0] && series[0].name;
      var cells = [];
      cats.forEach(function (r, i) {
        hRows.forEach(function (v, j) {
          cells.push([j, i, Number(v) || 0]);
        });
      });
      if (!cells.length) return null;
      var hv = cells.map(function (c) { return c[2]; });
      return shallow(base, {
        tooltip: {
          position: 'top', textStyle: { fontSize: 11, fontFamily: FONT_STACK },
          formatter: function (p) { return cats[p.value[1]] + '<br>' + hCats + ': ' + formatNumber(p.value[2]); }
        },
        legend: { show: false },
        grid: { left: 10, right: 12, top: gridTop, bottom: 40, containLabel: true },
        xAxis: shallow(catAxis, { data: series.map(function (s) { return s.name; }), splitArea: { show: true } }),
        yAxis: shallow(catAxis, { data: cats, splitArea: { show: true } }),
        visualMap: {
          min: Math.min.apply(null, hv), max: Math.max.apply(null, hv),
          calculable: true, orient: 'horizontal', left: 'center', bottom: 4,
          itemWidth: 10, itemHeight: 90, textStyle: { fontSize: 10 },
          inRange: { color: ['#e8f1fb', '#9ec5e8', '#3b82c4'] }
        },
        series: [{
          type: 'heatmap', data: cells,
          label: { show: true, fontSize: 9, formatter: function (p) { return formatNumber(p.value[2]); } },
          itemStyle: { borderColor: '#fff', borderWidth: 1 },
          emphasis: { itemStyle: { shadowBlur: 6, shadowColor: 'rgba(0,0,0,.2)' } }
        }]
      });
    }

    if (kind === 'wordcloud') {
      // The echarts-wordcloud plugin needs ECharts 5, but the bundled copy is
      // 4.1.0, so the cloud is laid out here instead: words are placed along
      // an Archimedean spiral and drawn on a scatter series.
      var words = cats.map(function (name, i) {
        return { name: name, value: Number((series[0] && series[0].data[i]) || 0) };
      }).filter(function (w) { return w.name && w.value > 0; });
      if (!words.length) return null;
      words.sort(function (a, b) { return b.value - a.value; });
      var top = words.slice(0, 80);
      var maxV = top[0].value || 1;
      var minV = top[top.length - 1].value || 0;
      var span = (maxV - minV) || 1;
      // Estimate the pixel size of each word from its length and weight.
      // The result is clamped so a degenerate weight range cannot produce an
      // unusable (zero or negative) font size.
      var sizes = top.map(function (w) {
        var ratio = (w.value - minV) / span;
        if (!isFinite(ratio) || ratio < 0) ratio = 0;
        if (ratio > 1) ratio = 1;
        return Math.max(10, Math.min(48, 12 + ratio * 34));
      });
      var placed = layoutWordCloud(top, sizes);
      if (!placed.length) return null;
      var cw = 520;
      var ch = Math.max(160, Math.ceil(placed.totalHeight || 320));
      return shallow(base, {
        tooltip: {
          trigger: 'item',
          textStyle: { fontSize: 11, fontFamily: FONT_STACK },
          formatter: function (p) {
            var d = p.data || {};
            return (d.name || '') + '<br>' + formatNumber(d.value);
          }
        },
        legend: { show: false },
        grid: { left: 4, right: 4, top: 24, bottom: 4 },
        xAxis: { type: 'value', min: 0, max: cw, show: false },
        yAxis: { type: 'value', min: 0, max: ch, show: false },
        series: [{
          type: 'scatter',
          data: placed,
          symbolSize: 0,
          label: {
            show: true,
            formatter: function (p) { return p.data.name; },
            fontSize: function (p) { return p.data.size; },
            color: function (p) { return p.data.color; },
            fontFamily: FONT_STACK,
            fontWeight: 'bold'
          },
          emphasis: { label: { fontSize: function (p) { return p.data.size + 3; } } }
        }]
      });
    }

    if (kind === 'map') {
      // Country/region geometry needs an offline map dataset that is not
      // bundled. Keep the data visible as a ranked bar list instead of an
      // empty frame, so the numbers are still readable.
      var ranked = [];
      cats.forEach(function (c, i) {
        var v = 0;
        series.forEach(function (s) { v += Number((s.data || [])[i]) || 0; });
        ranked.push({ name: c, value: v });
      });
      ranked = ranked.filter(function (r) { return r.name && r.value; });
      ranked.sort(function (a, b) { return b.value - a.value; });
      ranked = ranked.slice(0, 20).reverse();
      if (!ranked.length) return null;
      var areaNote = spec.mapArea ? spec.mapArea + (spec.drillDown ? '（钻取）' : '') : '';
      return shallow(base, {
        tooltip: { trigger: 'axis', axisPointer: { type: 'shadow' }, textStyle: { fontSize: 11, fontFamily: FONT_STACK } },
        legend: { show: false },
        title: hasTitle
          ? base.title
          : { show: true, text: areaNote, left: 'center', top: 4, textStyle: { fontSize: 12, color: '#6b7280', fontFamily: FONT_STACK } },
        grid: { left: 10, right: 24, top: 26, bottom: 6, containLabel: true },
        xAxis: shallow(valAxis, { name: areaNote || '', nameTextStyle: { fontSize: 10, color: '#98a2b3' } }),
        yAxis: shallow(catAxis, { data: ranked.map(function (r) { return r.name; }) }),
        series: [{
          type: 'bar', data: ranked.map(function (r) { return r.value; }),
          barMaxWidth: 18, itemStyle: { borderRadius: [0, 2, 2, 0] },
          label: { show: true, position: 'right', fontSize: 10, formatter: function (p) { return formatNumber(p.value); } }
        }]
      });
    }

    var horizontal = kind === 'bar' || kind === 'gantt' ? !!spec.horizontal : false;
    var catAxis = {
      type: 'category',
      data: cats,
      axisTick: { show: false },
      axisLine: { lineStyle: { color: '#d8dee8' } },
      axisLabel: {
        fontSize: 10,
        color: '#4b5563',
        hideOverlap: true,
        interval: 'auto',
        fontFamily: FONT_STACK
      }
    };
    var valAxis = {
      type: 'value',
      axisTick: { show: false },
      axisLine: { show: false },
      axisLabel: { fontSize: 10, color: '#6b7280', fontFamily: FONT_STACK },
      splitLine: { lineStyle: { color: '#eef1f5' } }
    };

    if (kind === 'box') {
      // A box plot series carries the five-number summary of every category
      // rather than plain values.
      var boxSeries = series.map(function (s) {
        var vals = s.boxValues || [];
        return {
          name: s.name || '',
          type: 'boxplot',
          data: vals.map(function (v) {
            return [v[0], v[1], v[2], v[3], v[4]];
          })
        };
      });
      if (!boxSeries.length) return null;
      var boxTop = 12;
      if (hasTitle) boxTop = titleBand + 6;
      if (showLegend && legendPos !== 'bottom' && legendPos !== 'left' && legendPos !== 'right') {
        boxTop += 18;
      }
      return shallow(base, {
        tooltip: {
          trigger: 'item',
          textStyle: { fontSize: 11, fontFamily: FONT_STACK },
          formatter: function (p) {
            var v = p.data || [];
            if (!v.length) return '';
            return (p.seriesName || '') + '<br>' +
              '最大值: ' + formatNumber(v[4]) + '<br>' +
              '上四分位: ' + formatNumber(v[3]) + '<br>' +
              '中位数: ' + formatNumber(v[2]) + '<br>' +
              '下四分位: ' + formatNumber(v[1]) + '<br>' +
              '最小值: ' + formatNumber(v[0]);
          }
        },
        legend: legend,
        grid: { left: 10, right: 12, top: boxTop, bottom: 6, containLabel: true },
        xAxis: shallow(catAxis, { type: 'category', data: cats }),
        yAxis: valAxis,
        series: boxSeries
      });
    }

    var gridTop = 12;
    if (hasTitle) gridTop = titleBand + 6;
    if (showLegend && legendPos !== 'bottom' && legendPos !== 'left' && legendPos !== 'right') {
      gridTop += 18;
    }
    var grid = {
      left: (showLegend && legendPos === 'left') ? 92 : 10,
      right: (showLegend && legendPos === 'right') ? 88 : 12,
      top: gridTop,
      bottom: (showLegend && legendPos === 'bottom') ? 32 : 6,
      containLabel: true
    };

    var chartSeries = series.map(function (s) {
      var type;
      if (kind === 'line' || kind === 'area') {
        type = 'line';
      } else if (kind === 'scatter' || kind === 'bubble') {
        type = 'scatter';
      } else {
        type = 'bar';
      }
      var o = {
        name: s.name || '',
        type: type,
        data: s.data || [],
        emphasis: { focus: 'series' }
      };
      if (type === 'line') {
        o.symbol = 'circle';
        o.symbolSize = 4;
        o.lineStyle = { width: 2 };
        if (kind === 'area') o.areaStyle = { opacity: 0.24 };
      } else if (type === 'bar') {
        o.barMaxWidth = 36;
        o.itemStyle = { borderRadius: horizontal ? [0, 2, 2, 0] : [2, 2, 0, 0] };
      } else {
        o.symbolSize = 6;
      }
      if (spec.stack && (type === 'bar' || type === 'line')) o.stack = 'total';
      return o;
    });

    // A gantt bar is a duration anchored at an absolute start, so it needs the
    // floating-bar form: [offset, offset + duration].
    if (kind === 'gantt') {
      var origin = (spec.axisValues && spec.axisValues.length) ? Number(spec.axisValues[0]) : 0;
      chartSeries = chartSeries.map(function (s) {
        return {
          name: s.name,
          type: 'bar',
          stack: undefined,
          data: (s.data || []).map(function (v) {
            return [origin, origin + Number(v || 0)];
          }),
          barMaxWidth: 18,
          itemStyle: { borderRadius: 2 },
          emphasis: { focus: 'series' }
        };
      });
    }

    return shallow(base, {
      tooltip: {
        trigger: 'axis',
        axisPointer: { type: kind === 'scatter' ? 'cross' : 'shadow' },
        textStyle: { fontSize: 11, fontFamily: FONT_STACK }
      },
      legend: legend,
      grid: grid,
      xAxis: horizontal ? valAxis : catAxis,
      yAxis: horizontal ? catAxis : valAxis,
      series: chartSeries
    });
  }

  function shallow(target, src) {
    Object.keys(src).forEach(function (k) { target[k] = src[k]; });
    return target;
  }

  // Lays out words by packing them into rows, largest first.
  //
  // A word cloud is not a scatter plot: what matters is that the words fill
  // the area without overlapping and that the biggest ones are near the
  // middle. Packing into rows achieves that reliably, while a spiral tends to
  // leave large empty gaps.
  function layoutWordCloud(words, sizes) {
    var W = 520;
    // The gap between two neighbouring words. It is applied as extra room
    // around a word rather than added to both neighbours, so the row width
    // stays an accurate sum.
    var gap = 8;
    var placed = [];
    var boxes = [];
    var palette = [PALETTE[0], '#f59e0b', '#10b981', '#ef4444', '#8b5cf6',
                   '#06b6d4', '#ec4899', '#84cc16', '#f97316', '#6366f1'];

    // Measure every word first.
    var items = [];
    for (var i = 0; i < words.length; i++) {
      var text = String(words[i].name);
      var size = sizes[i];
      if (!isFinite(size) || size <= 0) size = 12;
      size = Math.max(6, Math.min(72, size));
      // Chinese glyphs are square, latin ones are roughly half as wide.
      var wide = 0;
      for (var k = 0; k < text.length; k++) {
        wide += text.charCodeAt(k) > 0x2e80 ? 1 : 0.55;
      }
      items.push({
        name: text,
        value: words[i].value,
        size: size,
        w: wide * size * 0.62,
        h: size * 1.2,
        color: palette[i % palette.length]
      });
    }
    if (!items.length) return placed;

    // Pack into rows: start a new row when the current one is full. Each row
    // is sorted by size so the row height can be taken from its first (and
    // largest) word.
    var rows = [];
    var cur = [];
    var curW = 0;
    for (var m = 0; m < items.length; m++) {
      var need = cur.length ? gap : 0;
      if (curW + need + items[m].w > W && cur.length) {
        rows.push({ items: cur, w: curW });
        cur = [];
        curW = 0;
        need = 0;
      }
      cur.push(items[m]);
      curW += need + items[m].w;
    }
    if (cur.length) rows.push({ items: cur, w: curW });
    for (var rs = 0; rs < rows.length; rs++) {
      rows[rs].items.sort(function (a, b) { return b.size - a.size; });
    }

    // Centre each row, and stack the rows around the vertical middle.
    var y = 0;
    for (var r2 = 0; r2 < rows.length; r2++) {
      var row = rows[r2];
      var rowH = row.items[0].h + gap;
      // Centre the row. A row can be wider than the canvas when a single word
      // is very large, so the offset is clamped to keep everything on screen.
      var x = Math.max(0, (W - row.w) / 2);
      var cy = y + rowH / 2;
      for (var q = 0; q < row.items.length; q++) {
        var it = row.items[q];
        placed.push({
          name: it.name,
          value: it.value,
          x: x + it.w / 2,
          y: cy,
          size: Math.round(it.size),
          color: it.color
        });
        boxes.push({ x: x + it.w / 2, y: cy, bw: it.w, bh: it.h });
        x += it.w + gap;
      }
      y += rowH;
    }

    // Report the height so the caller can size the chart.
    placed.totalHeight = y;
    return placed;
  }

  function formatNumber(v) {
    if (!isFinite(v)) return String(v);
    var abs = Math.abs(v);
    if (abs >= 1e8) return (v / 1e8).toFixed(2).replace(/\.?0+$/, '') + '亿';
    if (abs >= 1e4) return (v / 1e4).toFixed(2).replace(/\.?0+$/, '') + '万';
    return Number(v.toFixed(2)).toString();
  }

  // ---------- zoom ----------

  function applyZoom() {
    var box = els.previewRoot.querySelector('.page-box');
    var scaleEl = els.previewRoot.querySelector('.page-scale');
    if (!box || !scaleEl || !state.pageW) return;
    var scale;
    if (state.fit) {
      var avail = Math.max(240, els.stage.clientWidth - 56);
      var fit = avail / state.pageW;
      scale = Math.min(1, fit);
      // Ultra-wide worksheets are unreadable when squeezed to the viewport;
      // show them at 100% and let the stage scroll instead.
      if (fit < 0.5) scale = 1;
      if (scale < 0.1) scale = 0.1;
    } else {
      scale = state.zoom;
    }
    state.scale = scale;
    scaleEl.style.transform = 'scale(' + scale + ')';
    box.style.width = Math.max(1, Math.round(state.pageW * scale)) + 'px';
    box.style.height = Math.max(1, Math.round(state.pageH * scale)) + 'px';
    els.zoomLabel.textContent = Math.round(scale * 100) + '%';
  }

  // ---------- utilities ----------

  function prefixSums(values) {
    var out = [0];
    var sum = 0;
    for (var i = 0; i < values.length; i++) {
      sum += Number(values[i]) || 0;
      out.push(sum);
    }
    return out;
  }

  // The address bar hash carries the preview link:
  //   #path=<模板路径>&<参数名>=<参数值>
  // It is what people copy to share a report with a particular set of
  // parameters.
  function readHash() {
    try {
      var raw = location.hash.replace(/^#/, '');
      if (!raw) return null;
      var qs = new URLSearchParams(raw);
      var path = qs.get('path');
      if (!path) return null;
      var params = {};
      qs.forEach(function (value, key) {
        if (key === 'path') return;
        params[key] = value;
      });
      return { path: path, params: params };
    } catch (e) {
      return null;
    }
  }

  // Writes the current preview into the address bar so the link can be copied.
  function syncHash() {
    if (!state.path) return;
    var qs = new URLSearchParams();
    qs.set('path', state.path);
    Object.keys(state.params || {}).forEach(function (k) {
      var v = state.params[k];
      if (v !== undefined && v !== null && v !== '') qs.set(k, v);
    });
    var next = '#' + qs.toString();
    if (location.hash !== next) {
      history.replaceState(null, '', next);
    }
  }

  function basename(path) {
    if (!path) return '';
    var parts = String(path).split(/[\\/]/);
    return parts[parts.length - 1];
  }

  function formatSize(size) {
    if (!size || size < 0) return '';
    if (size < 1024) return size + ' B';
    if (size < 1024 * 1024) return (size / 1024).toFixed(1) + ' KB';
    return (size / 1024 / 1024).toFixed(1) + ' MB';
  }

  function toast(message) {
    var old = document.querySelector('.toast');
    if (old) old.remove();
    var el = document.createElement('div');
    el.className = 'toast';
    el.textContent = message;
    document.body.appendChild(el);
    clearTimeout(toastTimer);
    toastTimer = setTimeout(function () {
      el.remove();
    }, 4600);
  }
})();
