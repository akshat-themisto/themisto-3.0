(() => {
  const SURFACE = 'browser_safari';
  const API_BASE = 'http://127.0.0.1:17175';
  const MAX_PROMPT_CHARS = 120000;
  const EVALUATE_TIMEOUT_MS = 3000;
  const SUPPORT_HOSTS = [
    'claude.ai',
    'chatgpt.com',
    'chat.openai.com',
    'gemini.google.com',
    'grok.com',
    'x.com',
    'twitter.com',
    'x.ai',
  ];

  const host = window.location.hostname.toLowerCase();
  if (!SUPPORT_HOSTS.some((h) => host === h || host.endsWith('.' + h))) {
    return;
  }

  let evaluationInFlight = false;

  function inferVendor() {
    if (host.includes('openai') || host.includes('chatgpt')) return 'openai';
    if (host.includes('claude') || host.includes('anthropic')) return 'anthropic';
    if (host.includes('gemini') || host.includes('google')) return 'google';
    if (host.includes('grok') || host === 'x.com' || host.endsWith('.x.com') || host === 'twitter.com' || host.endsWith('.twitter.com') || host === 'x.ai' || host.endsWith('.x.ai')) return 'xai';
    return '';
  }

  function isSupportedRoute() {
    if (host === 'x.com' || host.endsWith('.x.com') || host === 'twitter.com' || host.endsWith('.twitter.com')) {
      const path = (window.location.pathname || '').toLowerCase();
      return path.includes('/grok');
    }
    return true;
  }

  function isVisible(el) {
    if (!(el instanceof Element)) {
      return false;
    }
    const style = window.getComputedStyle(el);
    if (style.display === 'none' || style.visibility === 'hidden') {
      return false;
    }
    const rect = el.getBoundingClientRect();
    return rect.width > 0 && rect.height > 0;
  }

  function isPromptField(el) {
    if (!(el instanceof Element)) {
      return false;
    }
    const tag = el.tagName.toLowerCase();
    if (tag === 'textarea') {
      return true;
    }
    const role = (el.getAttribute('role') || '').toLowerCase();
    if (role === 'textbox') {
      return true;
    }
    const editable = (el.getAttribute('contenteditable') || '').toLowerCase();
    if (editable === 'true' || editable === 'plaintext-only') {
      return true;
    }
    return el.classList.contains('ProseMirror');
  }

  function pickPromptField(scope) {
    const root = scope || document;
    const candidates = Array.from(
      root.querySelectorAll('textarea, [contenteditable="true"], [contenteditable="plaintext-only"], div[role="textbox"], .ProseMirror')
    );
    const visible = candidates.filter((el) => isPromptField(el) && isVisible(el));
    if (visible.length === 0) {
      return null;
    }

    const active = document.activeElement;
    if (active && visible.includes(active)) {
      return active;
    }

    visible.sort((a, b) => readPromptText(b).length - readPromptText(a).length);
    return visible[0] || null;
  }

  function readPromptText(el) {
    if (!el) return '';
    if (typeof el.value === 'string') {
      return el.value.trim();
    }
    return (el.innerText || el.textContent || '').trim();
  }

  function normalizePrompt(prompt) {
    if (!prompt) return '';
    if (prompt.length <= MAX_PROMPT_CHARS) return prompt;
    return prompt.slice(0, MAX_PROMPT_CHARS);
  }

  function showBanner(kind, message) {
    const existing = document.getElementById('themisto-prompt-banner');
    if (existing) {
      existing.remove();
    }

    const node = document.createElement('div');
    node.id = 'themisto-prompt-banner';
    node.textContent = message;
    node.style.position = 'fixed';
    node.style.top = '14px';
    node.style.left = '50%';
    node.style.transform = 'translateX(-50%)';
    node.style.zIndex = '2147483647';
    node.style.fontFamily = 'ui-sans-serif, -apple-system, Segoe UI, Helvetica, Arial, sans-serif';
    node.style.fontSize = '13px';
    node.style.padding = '10px 14px';
    node.style.borderRadius = '10px';
    node.style.border = '1px solid rgba(0,0,0,.2)';
    node.style.boxShadow = '0 10px 30px rgba(0,0,0,.22)';
    node.style.maxWidth = '70vw';

    if (kind === 'block') {
      node.style.background = '#fee2e2';
      node.style.color = '#7f1d1d';
      node.style.borderColor = '#fca5a5';
    } else if (kind === 'warn') {
      node.style.background = '#fef3c7';
      node.style.color = '#78350f';
      node.style.borderColor = '#fcd34d';
    } else {
      node.style.background = '#e0f2fe';
      node.style.color = '#0c4a6e';
      node.style.borderColor = '#7dd3fc';
    }

    document.body.appendChild(node);
    window.setTimeout(() => {
      node.remove();
    }, 4600);
  }

  async function postJSON(path, payload, timeoutMs) {
    const resp = await browser.runtime.sendMessage({
      type: 'themisto_fetch',
      url: API_BASE + path,
      body: JSON.stringify(payload),
      timeoutMs,
    });
    if (resp && resp.error) {
      throw new Error(resp.error);
    }
    return resp.data;
  }

  async function reportOutcome(result, outcome, errorText) {
    const payload = {
      evaluation_id: result && result.evaluation_id ? result.evaluation_id : '',
      surface: SURFACE,
      outcome,
      destination_host: host,
      destination_path: window.location.pathname || '/',
      vendor: inferVendor(),
      app_name: (() => {
        const brands = navigator.userAgentData && Array.isArray(navigator.userAgentData.brands) ? navigator.userAgentData.brands.map((b) => b.brand).join(', ') : '';
        return brands || navigator.userAgent;
      })(),
      user_message_shown: true,
      error: errorText || '',
    };

    try {
      await postJSON('/v1/prompt/outcome', payload, EVALUATE_TIMEOUT_MS);
    } catch (_) {
      // Best effort only.
    }
  }

  async function evaluatePrompt(promptText) {
    const payload = {
      prompt_text: normalizePrompt(promptText),
      surface: SURFACE,
      app_name: document.title || navigator.userAgent,
      destination_url: window.location.href,
      destination_host: host,
      destination_path: window.location.pathname || '/',
      vendor: inferVendor(),
      service_category: 'ai_llm',
      protocol: 'http',
      adapter_version: '1.0.0',
    };
    return await postJSON('/v1/prompt/evaluate', payload, EVALUATE_TIMEOUT_MS);
  }

  function markBypassForm(form) {
    if (form instanceof HTMLFormElement) {
      form.dataset.themistoBypass = '1';
    }
  }

  function clearBypassForm(form) {
    if (form instanceof HTMLFormElement && form.dataset.themistoBypass === '1') {
      form.dataset.themistoBypass = '';
      return true;
    }
    return false;
  }

  function markBypassButton(button) {
    if (button instanceof HTMLElement) {
      button.dataset.themistoBypass = '1';
    }
  }

  function clearBypassButton(button) {
    if (button instanceof HTMLElement && button.dataset.themistoBypass === '1') {
      button.dataset.themistoBypass = '';
      return true;
    }
    return false;
  }

  function looksLikeSendButton(button) {
    if (!(button instanceof Element)) {
      return false;
    }
    if (!isVisible(button)) {
      return false;
    }

    if (button instanceof HTMLButtonElement && button.type && button.type.toLowerCase() === 'submit') {
      return true;
    }

    const textBlob = [
      button.getAttribute('aria-label') || '',
      button.getAttribute('title') || '',
      button.getAttribute('data-testid') || '',
      button.getAttribute('name') || '',
      button.id || '',
      button.className || '',
      button.textContent || '',
    ].join(' ').toLowerCase();

    const positive = /(send|submit|send prompt|send message|arrow up|paper plane)/.test(textBlob);
    const negative = /(stop|cancel|voice|microphone|mic|attach|upload|new chat|new conversation)/.test(textBlob);
    return positive && !negative;
  }

  function findSendButton(field, form, preferredRoot) {
    const roots = [];
    if (preferredRoot instanceof Element) roots.push(preferredRoot);
    if (form instanceof HTMLFormElement) roots.push(form);
    if (field instanceof Element) {
      const composer = field.closest('[data-testid*="composer"], [class*="composer"], [role="form"], form');
      if (composer instanceof Element) roots.push(composer);
    }
    roots.push(document);

    const seen = new Set();
    for (const root of roots) {
      if (!root || seen.has(root)) {
        continue;
      }
      seen.add(root);
      const candidates = Array.from(root.querySelectorAll('button, [role="button"], input[type="submit"], input[type="button"]'));
      for (const candidate of candidates) {
        if (looksLikeSendButton(candidate)) {
          return candidate;
        }
      }
    }
    return null;
  }

  function submitWithBypass(form) {
    if (!(form instanceof HTMLFormElement)) {
      return false;
    }
    markBypassForm(form);
    form.requestSubmit();
    return true;
  }

  function clickWithBypass(button, form) {
    if (!(button instanceof HTMLElement)) {
      return false;
    }
    markBypassButton(button);
    markBypassForm(form);
    button.click();
    return true;
  }

  function resumeSend(action) {
    if (action.kind === 'submit') {
      return submitWithBypass(action.form);
    }
    if (action.kind === 'click') {
      return clickWithBypass(action.button, action.form);
    }

    const button = findSendButton(action.field, action.form, action.root);
    if (button) {
      return clickWithBypass(button, action.form);
    }
    return submitWithBypass(action.form);
  }

  function shouldInterceptEnterSend(event, target, field) {
    if (event.key !== 'Enter' || event.isComposing) {
      return false;
    }
    if (event.shiftKey && !event.ctrlKey && !event.metaKey) {
      return false;
    }
    if (!isPromptField(field)) {
      return false;
    }
    if (target instanceof Element && target !== field && !field.contains(target)) {
      return false;
    }
    return true;
  }

  function actionFromEventTarget(target) {
    const t = target instanceof Element ? target : null;
    const root = t ? t.closest('[data-testid*="composer"], [class*="composer"], [role="form"], form') : null;
    const form = t && t.closest('form') instanceof HTMLFormElement ? t.closest('form') : null;
    const field = pickPromptField(root || form || document) || pickPromptField(document);
    return { root, form, field };
  }

  async function enforceAndMaybeResume(promptText, action) {
    if (evaluationInFlight) {
      showBanner('warn', 'Prompt check already in progress.');
      return;
    }
    evaluationInFlight = true;

    let result;
    try {
      result = await evaluatePrompt(promptText);
    } catch (err) {
      showBanner('warn', 'Prompt evaluator unavailable. Send allowed in degraded fail-open mode.');
      await reportOutcome(null, 'degraded_fail_open', String(err));
      resumeSend(action);
      evaluationInFlight = false;
      return;
    }

    if (result.decision === 'block' || result.outcome === 'blocked') {
      showBanner('block', result.message || 'Prompt blocked by policy.');
      await reportOutcome(result, 'blocked', '');
      evaluationInFlight = false;
      return;
    }

    if (result.decision === 'alert') {
      showBanner('warn', result.message || 'Prompt allowed with policy alert.');
      await reportOutcome(result, 'allowed', '');
    } else if (result.outcome === 'degraded_fail_open' || result.degraded) {
      showBanner('warn', result.message || 'Prompt evaluation degraded. Send allowed.');
      await reportOutcome(result, 'degraded_fail_open', result.degraded_cause || 'degraded_fail_open');
    } else {
      await reportOutcome(result, 'allowed', '');
    }

    if (!resumeSend(action)) {
      showBanner('warn', 'Prompt allowed, but auto-send resume failed. Press Send again.');
    }
    evaluationInFlight = false;
  }

  async function processFormSubmit(event) {
    if (!isSupportedRoute()) {
      return;
    }
    const form = event.target;
    if (!(form instanceof HTMLFormElement)) {
      return;
    }
    if (clearBypassForm(form)) {
      return;
    }

    const field = pickPromptField(form) || pickPromptField(document);
    const promptText = readPromptText(field);
    if (!promptText) {
      return;
    }

    event.preventDefault();
    event.stopImmediatePropagation();

    await enforceAndMaybeResume(promptText, {
      kind: 'submit',
      form,
      field,
      root: form,
    });
  }

  async function processSendButtonClick(event) {
    if (!isSupportedRoute()) {
      return;
    }
    const target = event.target;
    if (!(target instanceof Element)) {
      return;
    }
    const button = target.closest('button, [role="button"], input[type="submit"], input[type="button"]');
    if (!(button instanceof Element)) {
      return;
    }
    if (!looksLikeSendButton(button)) {
      return;
    }
    if (clearBypassButton(button)) {
      return;
    }

    const context = actionFromEventTarget(button);
    const promptText = readPromptText(context.field);
    if (!promptText) {
      return;
    }

    event.preventDefault();
    event.stopImmediatePropagation();

    await enforceAndMaybeResume(promptText, {
      kind: 'click',
      button,
      form: context.form,
      field: context.field,
      root: context.root,
    });
  }

  async function processKeydown(event) {
    if (!isSupportedRoute()) {
      return;
    }
    const target = event.target;
    const context = actionFromEventTarget(target);
    if (!shouldInterceptEnterSend(event, target, context.field)) {
      return;
    }

    const promptText = readPromptText(context.field);
    if (!promptText) {
      return;
    }

    event.preventDefault();
    event.stopImmediatePropagation();

    await enforceAndMaybeResume(promptText, {
      kind: 'keydown',
      form: context.form,
      field: context.field,
      root: context.root,
    });
  }

  document.addEventListener('submit', (event) => {
    void processFormSubmit(event);
  }, true);

  document.addEventListener('click', (event) => {
    void processSendButtonClick(event);
  }, true);

  document.addEventListener('keydown', (event) => {
    void processKeydown(event);
  }, true);
})();
