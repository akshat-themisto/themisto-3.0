chrome.runtime.onMessage.addListener((msg, sender, sendResponse) => {
  if (msg.type !== 'themisto_fetch') return false;

  const controller = new AbortController();
  const timeout = setTimeout(() => controller.abort('timeout'), msg.timeoutMs || 2000);

  fetch(msg.url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: msg.body,
    signal: controller.signal,
  })
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
