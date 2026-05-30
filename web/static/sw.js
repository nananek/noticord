/* noticord Service Worker: Web Push の受信と通知クリック処理 */

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (event) => event.waitUntil(self.clients.claim()));

self.addEventListener('push', (event) => {
  // iOS は push 受信時に必ず1件通知を出さないと購読を剥奪する。
  // データ欠落・パース失敗でも確実に showNotification を呼べるよう全体を try で包む。
  const show = async () => {
    let data = { title: 'noticord', body: '', url: '/' };
    try {
      if (event.data) {
        try { data = Object.assign(data, event.data.json()); }
        catch (_) { data.body = event.data.text(); }
      }
    } catch (_) { /* 取り出し失敗時もフォールバック通知を出す */ }

    const options = {
      body: data.body || 'New notification',
      icon: '/icon.svg',
      badge: '/icon.svg',
      tag: data.tag || 'noticord',
      data: { url: data.url || '/' },
      renotify: true,
      // iOS でロック画面に残りやすくするため timestamp を付与。
      timestamp: Date.now(),
    };
    try {
      await self.registration.showNotification(data.title || 'noticord', options);
    } catch (_) {
      // 最低限のフォールバック(購読剥奪を避けるため必ず1件出す)。
      await self.registration.showNotification('noticord', { body: 'New notification', icon: '/icon.svg' });
    }
  };
  event.waitUntil(show());
});

self.addEventListener('notificationclick', (event) => {
  event.notification.close();
  const targetUrl = (event.notification.data && event.notification.data.url) || '/';
  // URL から ?c=<channel> を抽出して、既存ウィンドウにチャンネル切替を伝える。
  let channel = '';
  try { channel = new URL(targetUrl, self.location.origin).searchParams.get('c') || ''; } catch (_) {}

  event.waitUntil(
    self.clients.matchAll({ type: 'window', includeUncontrolled: true }).then((clients) => {
      for (const c of clients) {
        if ('focus' in c) {
          c.postMessage(channel ? { type: 'navigate', channel } : { type: 'refresh' });
          return c.focus();
        }
      }
      if (self.clients.openWindow) return self.clients.openWindow(targetUrl);
    })
  );
});
