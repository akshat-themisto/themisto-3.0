browser.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.type !== 'themisto_fetch') return false;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort('timeout'), msg.timeoutMs || 2000);

  const method = msg.method || 'POST';
  const init = {
    method,
    headers: { 'Content-Type': 'application/json' },
    signal: controller.signal,
  };
  if (method !== 'GET' && msg.body) {
    init.body = msg.body;
  }

  fetch(msg.url, init)
    .then(async (resp) => {
      clearTimeout(timeout);
      if (!resp.ok) {
        sendResponse({ error: 'http_' + resp.status });
        return;
      }
      const data = await resp.json();
      sendResponse({ data });
    })
    .catch((err) => {
      clearTimeout(timeout);
      sendResponse({ error: String(err) });
    });

  return true;
});
