'use strict';

const $ = (id) => document.getElementById(id);

// ---- 状態 ----
const state = {
  channels: [],
  current: null,   // 選択中チャンネル id
  authRequired: false,
  unread: {},      // channelId -> 未読件数
  es: null,        // EventSource
  // ?debug=1 か localStorage.debug=1 でクライアント側 SSE ログを有効化。
  debug: new URL(location.href).searchParams.get('debug') === '1' ||
    (typeof localStorage !== 'undefined' && localStorage.getItem('debug') === '1'),
};

// ---- 汎用 ----
function toast(msg, ms) {
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toast._t);
  // 複数行(テスト結果など)は長めに表示する。
  const dur = ms || (msg.indexOf('\n') >= 0 ? 6000 : 2500);
  toast._t = setTimeout(() => t.classList.remove('show'), dur);
}

async function api(path, opts = {}) {
  const res = await fetch(path, {
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    ...opts,
  });
  if (res.status === 401) {
    showLogin();
    throw new Error('unauthorized');
  }
  return res;
}

function fmtTime(unix) {
  if (!unix) return '';
  return new Date(unix * 1000).toLocaleString('ja-JP', { hour12: false });
}
function fmtIso(s) {
  const d = new Date(s);
  return isNaN(d) ? s : d.toLocaleString('ja-JP', { hour12: false });
}
function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}
const escapeAttr = escapeHtml;
function safeUrl(u) {
  return typeof u === 'string' && /^https?:\/\//i.test(u.trim());
}
async function copyText(text) {
  try { await navigator.clipboard.writeText(text); toast('コピーしました'); }
  catch (_) { toast('コピーできませんでした'); }
}

// ---- 簡易 Markdown ----
function markdownInline(s) {
  let t = escapeHtml(String(s));
  t = t.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g,
    (_, text, url) => `<a href="${url}" target="_blank" rel="noopener">${text}</a>`);
  t = t.replace(/`([^`]+)`/g, '<code>$1</code>');
  t = t.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  t = t.replace(/__([^_]+)__/g, '<u>$1</u>');
  t = t.replace(/~~([^~]+)~~/g, '<s>$1</s>');
  t = t.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  return t;
}
function markdown(s) { return markdownInline(String(s)).replace(/\n/g, '<br>'); }

// ---- 画面切り替え ----
function showLogin() {
  $('loginScreen').classList.remove('hidden');
  $('app').classList.add('hidden');
}
function showApp() {
  $('loginScreen').classList.add('hidden');
  $('app').classList.remove('hidden');
}

// ---- 認証 ----
async function login() {
  $('loginError').textContent = '';
  const res = await fetch('/api/login', {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: $('password').value }),
  });
  if (res.ok) { $('password').value = ''; await init(); }
  else { $('loginError').textContent = 'パスワードが違います'; }
}
async function logout() {
  disconnectSSE(); // 認証切れ後に繋ぎっぱなしにしない
  await api('/api/logout', { method: 'POST' });
  showLogin();
}

// ---- 通知(Web Push) ----
let swReg = null;

function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

// iOS / iPadOS の判定(iPadOS は Mac を騙るのでタッチ有無で補足)。
function isIOS() {
  const ua = navigator.userAgent || '';
  return /iPad|iPhone|iPod/.test(ua) ||
    (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1);
}

// ホーム画面に追加した PWA として起動しているか。
function isStandalone() {
  return window.matchMedia('(display-mode: standalone)').matches ||
    window.navigator.standalone === true;
}

// Push が使える環境か(SW + PushManager + Notification が揃っているか)。
function pushSupported() {
  return 'serviceWorker' in navigator && 'PushManager' in window && 'Notification' in window;
}

// iOS でタブから開いている等、鳴らない状態のときに誘導バナーを出す。
function updateGuidance() {
  const el = $('pushGuide');
  if (!el) return;
  // 既に通知 ON なら案内不要。
  if (Notification && Notification.permission === 'granted' && pushSupported()) {
    el.classList.add('hidden');
    return;
  }
  let msg = '';
  if (isIOS() && !isStandalone()) {
    // iOS はホーム画面 PWA でしか Web Push が動かない。
    msg = '📲 iPhone / iPad では、共有メニューの「ホーム画面に追加」で追加し、' +
      'その<b>アイコンから起動</b>してから通知を有効化してください。Safari のタブでは通知が鳴りません。';
  } else if (!pushSupported()) {
    if (isIOS()) {
      msg = '📲 この端末では通知に未対応です。iOS 16.4 以上にして、' +
        'ホーム画面に追加したアイコンから起動してください。';
    } else {
      msg = 'このブラウザは Web Push に対応していません。';
    }
  }
  if (msg) { el.innerHTML = msg; el.classList.remove('hidden'); }
  else el.classList.add('hidden');
}

async function refreshNotifStatus() {
  const dot = $('notifDot');
  updateGuidance();
  if (!pushSupported()) {
    dot.textContent = '非対応'; dot.className = 'status off';
    // iOS タブ時は「有効化」を押させて誘導を出したいので disabled にはしない。
    $('enableBtn').disabled = !isIOS();
    return;
  }
  $('enableBtn').disabled = false;
  swReg = await navigator.serviceWorker.getRegistration();
  const sub = swReg ? await swReg.pushManager.getSubscription() : null;
  if (sub && Notification.permission === 'granted') {
    dot.textContent = '通知 ON'; dot.className = 'status ok';
    $('enableBtn').classList.add('hidden');
    $('disableBtn').classList.remove('hidden');
  } else {
    dot.textContent = '通知 OFF'; dot.className = 'status off';
    $('enableBtn').classList.remove('hidden');
    $('disableBtn').classList.add('hidden');
  }
}

async function enableNotifications() {
  // iOS でタブ起動だと PushManager が無く、購読しても鳴らない。先に誘導する。
  if (isIOS() && !isStandalone()) {
    updateGuidance();
    toast('まず「ホーム画面に追加」して、そのアイコンから開いてください');
    return;
  }
  if (!pushSupported()) {
    toast('このブラウザはWeb Push非対応です');
    updateGuidance();
    return;
  }
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') {
    toast(perm === 'denied'
      ? '通知がブロックされています。端末の設定から許可してください'
      : '通知が許可されませんでした');
    return;
  }
  swReg = await navigator.serviceWorker.register('/sw.js');
  await navigator.serviceWorker.ready;
  try {
    const { key } = await (await api('/api/vapid-public-key')).json();
    const sub = await swReg.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(key),
    });
    await api('/api/subscribe', { method: 'POST', body: JSON.stringify(sub.toJSON()) });
    toast('通知を有効化しました');
  } catch (e) {
    toast('購読に失敗しました: ' + (e && e.message ? e.message : e));
  }
  await refreshNotifStatus();
}

async function disableNotifications() {
  if (!swReg) swReg = await navigator.serviceWorker.getRegistration();
  const sub = swReg ? await swReg.pushManager.getSubscription() : null;
  if (sub) {
    await api('/api/unsubscribe', { method: 'POST', body: JSON.stringify({ endpoint: sub.endpoint }) });
    await sub.unsubscribe();
  }
  toast('通知を無効化しました');
  await refreshNotifStatus();
}

async function testPush() {
  const d = await (await api('/api/test', { method: 'POST' })).json();
  // デバイスごとの結果(apple/fcm + ステータス)を要約して見せる。
  let detail = '';
  if (Array.isArray(d.results) && d.results.length) {
    detail = '\n' + d.results.map((r) => {
      const where = (r.host || '').includes('apple') ? 'iPhone(apple)'
        : (r.host || '').includes('google') ? 'Android/Chrome(fcm)'
        : (r.host || 'device');
      const st = r.error ? `ERR ${r.error}` : (r.pruned ? `失効(${r.status})` : `OK ${r.status}`);
      return `・${where}: ${st}`;
    }).join('\n');
  }
  toast(`送信: ${d.sent}/${d.subscriptions} 件${detail}`);
}

// ---- チャンネル ----
async function loadChannels(selectId) {
  state.channels = await (await api('/api/channels')).json();
  renderChannelList();
  // 選択チャンネルを決める: 指定 > 既存維持 > 先頭
  let target = selectId || state.current;
  if (!state.channels.find((c) => c.id === target)) {
    target = state.channels.length ? state.channels[0].id : null;
  }
  if (target) await selectChannel(target);
  else renderEmptyMain();
}

function renderChannelList() {
  const el = $('channelList');
  el.innerHTML = '';
  for (const c of state.channels) {
    const div = document.createElement('div');
    div.className = 'channel-item' + (c.id === state.current ? ' active' : '');
    const n = state.unread[c.id] || 0;
    const badge = n > 0 ? `<span class="badge">${n > 99 ? '99+' : n}</span>` : '';
    div.innerHTML = `
      <span class="hash">#</span>
      <span class="cname">${escapeHtml(c.name)}</span>${badge}`;
    div.onclick = () => { selectChannel(c.id); $('app').classList.remove('drawer-open'); };
    el.appendChild(div);
  }
}

function renderEmptyMain() {
  $('curChannelName').textContent = '—';
  $('curTopic').textContent = '';
  $('timeline').innerHTML = '<p class="center">「＋」からチャンネルを作成してください</p>';
}

async function selectChannel(id) {
  state.current = id;
  state.unread[id] = 0; // 開いたら未読クリア
  const ch = state.channels.find((c) => c.id === id);
  renderChannelList();
  if (ch) {
    $('curChannelName').textContent = ch.name;
    $('curTopic').textContent = ch.topic || '';
  }
  // URL に反映(通知クリック動線と同形式)
  const u = new URL(location.href);
  u.searchParams.set('c', id);
  history.replaceState(null, '', u);
  await loadMessages();
}

async function createChannel() {
  const name = $('newChannelName').value;
  if (!name.trim()) { toast('名前を入力してください'); return; }
  const res = await api('/api/channels', { method: 'POST', body: JSON.stringify({
    name, topic: $('newChannelTopic').value,
  }) });
  if (!res.ok) { const e = await res.json(); toast(e.error || '作成失敗'); return; }
  const c = await res.json();
  $('newChannelName').value = ''; $('newChannelTopic').value = '';
  closeModals();
  await loadChannels(c.id);
  toast(`#${c.name} を作成しました`);
}

// ---- メッセージ ----
async function loadMessages() {
  if (!state.current) return;
  const msgs = await (await api(`/api/channels/${state.current}/messages?limit=100`)).json();
  const el = $('timeline');
  el.innerHTML = '';
  if (!msgs.length) {
    el.innerHTML = '<p class="center">まだ通知はありません。<br>このチャンネルのWebhook URLに送ると、ここに表示されます。</p>';
    return;
  }
  // 古い→新しいの順に縦に並べる(チャットらしく)
  msgs.reverse();
  for (const m of msgs) el.appendChild(renderMessage(m));
  el.scrollTop = el.scrollHeight;
}

// appendMessage は SSE で届いた1件を現在のタイムライン末尾に追記する。
// 一番下付近を見ているときだけ自動スクロールする(上を読んでいる最中の妨げ防止)。
function appendMessage(m) {
  const el = $('timeline');
  // 空状態プレースホルダを除去。
  const ph = el.querySelector('.center');
  if (ph) el.innerHTML = '';
  const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
  el.appendChild(renderMessage(m));
  if (nearBottom) el.scrollTop = el.scrollHeight;
}

function renderMessage(m) {
  const row = document.createElement('div');
  row.className = 'message-row';

  const name = m.username || 'noticord';
  const initial = escapeHtml(name.slice(0, 1).toUpperCase());
  const avatar = safeUrl(m.avatar)
    ? `<img src="${escapeAttr(m.avatar)}" alt="">`
    : initial;

  let embeds = [];
  if (m.embeds && m.embeds !== '[]') {
    try { embeds = JSON.parse(m.embeds) || []; } catch (_) {}
  }
  const text = m.content ? `<div class="text">${markdown(m.content)}</div>` : '';
  const embedHtml = embeds.map(renderEmbed).join('');

  row.innerHTML = `
    <div class="avatar">${avatar}</div>
    <div class="body">
      <div class="meta">
        <span class="author">${escapeHtml(name)}</span>
        <span class="time">${fmtTime(m.created_at)}</span>
        <button class="ghost del" data-del="${m.id}" title="削除" style="padding:0 8px">×</button>
      </div>
      ${text}${embedHtml}
    </div>`;
  row.querySelector('[data-del]').onclick = async () => {
    await api('/api/messages/' + m.id, { method: 'DELETE' });
    await loadMessages();
  };
  return row;
}

// embed を Discord 風カードにレンダリング
function renderEmbed(e) {
  if (!e || typeof e !== 'object') return '';
  const color = typeof e.color === 'number' && e.color > 0
    ? '#' + e.color.toString(16).padStart(6, '0') : '#4f545c';
  let inner = '';

  if (e.author && e.author.name) {
    const icon = safeUrl(e.author.icon_url)
      ? `<img class="embed-author-icon" src="${escapeAttr(e.author.icon_url)}" alt="">` : '';
    const name = escapeHtml(e.author.name);
    inner += `<div class="embed-author">${icon}${
      safeUrl(e.author.url) ? `<a href="${escapeAttr(e.author.url)}" target="_blank" rel="noopener">${name}</a>` : name
    }</div>`;
  }
  if (e.title) {
    const t = markdownInline(e.title);
    inner += `<div class="embed-title">${
      safeUrl(e.url) ? `<a href="${escapeAttr(e.url)}" target="_blank" rel="noopener">${t}</a>` : t
    }</div>`;
  }
  if (e.description) inner += `<div class="embed-desc">${markdown(e.description)}</div>`;

  if (Array.isArray(e.fields) && e.fields.length) {
    const fields = e.fields.map((f) => `
      <div class="embed-field${f.inline ? ' inline' : ''}">
        <div class="embed-field-name">${markdownInline(f.name || '')}</div>
        <div class="embed-field-value">${markdown(f.value || '')}</div>
      </div>`).join('');
    inner += `<div class="embed-fields">${fields}</div>`;
  }
  if (e.image && safeUrl(e.image.url)) {
    inner += `<a href="${escapeAttr(e.image.url)}" target="_blank" rel="noopener"><img class="embed-image" src="${escapeAttr(e.image.url)}" alt=""></a>`;
  }
  let thumb = '';
  if (e.thumbnail && safeUrl(e.thumbnail.url)) {
    thumb = `<img class="embed-thumb" src="${escapeAttr(e.thumbnail.url)}" alt="">`;
  }
  if ((e.footer && e.footer.text) || e.timestamp) {
    const icon = e.footer && safeUrl(e.footer.icon_url)
      ? `<img class="embed-footer-icon" src="${escapeAttr(e.footer.icon_url)}" alt="">` : '';
    const txt = e.footer && e.footer.text ? escapeHtml(e.footer.text) : '';
    const ts = e.timestamp ? `<span class="embed-ts">${escapeHtml(fmtIso(e.timestamp))}</span>` : '';
    const sep = txt && ts ? ' • ' : '';
    inner += `<div class="embed-footer">${icon}<span>${txt}${sep}</span>${ts}</div>`;
  }
  const body = thumb
    ? `<div class="embed-grid"><div class="embed-main">${inner}</div>${thumb}</div>` : inner;
  return `<div class="embed" style="border-left-color:${color}">${body}</div>`;
}

// ---- チャンネル設定モーダル ----
async function openSettings() {
  if (!state.current) { toast('チャンネルがありません'); return; }
  const ch = state.channels.find((c) => c.id === state.current);
  if (!ch) return;
  $('setChName').textContent = ch.name;
  $('setName').value = ch.name;
  $('setTopic').value = ch.topic || '';
  await loadWebhooks();
  openModal('settingsModal');
}

async function saveChannel() {
  const res = await api('/api/channels/' + state.current, { method: 'PATCH', body: JSON.stringify({
    name: $('setName').value, topic: $('setTopic').value,
  }) });
  if (!res.ok) { toast('保存失敗'); return; }
  closeModals();
  await loadChannels(state.current);
  toast('保存しました');
}

async function deleteChannel() {
  const ch = state.channels.find((c) => c.id === state.current);
  if (!confirm(`#${ch ? ch.name : ''} を削除しますか?\nWebhookと履歴もすべて削除されます。`)) return;
  const res = await api('/api/channels/' + state.current, { method: 'DELETE' });
  if (!res.ok) { const e = await res.json(); toast(e.error || '削除失敗'); return; }
  closeModals();
  state.current = null;
  await loadChannels();
  toast('チャンネルを削除しました');
}

async function loadWebhooks() {
  const list = await (await api(`/api/channels/${state.current}/webhooks`)).json();
  const el = $('webhookList');
  el.innerHTML = '';
  if (!list.length) { el.innerHTML = '<p class="muted">まだWebhookがありません。</p>'; return; }
  for (const t of list) {
    const used = t.last_used_at ? `最終利用 ${fmtTime(t.last_used_at)}` : '未使用';
    const div = document.createElement('div');
    div.className = 'wh';
    div.innerHTML = `
      <div class="row"><strong class="grow">${escapeHtml(t.name || '(無名)')}</strong>
        <button class="danger" data-del="${t.id}">削除</button></div>
      <div class="url">${escapeHtml(t.url)}</div>
      <div class="row"><button class="secondary" data-copy>コピー</button>
        <span class="muted">${used}</span></div>`;
    div.querySelector('[data-copy]').onclick = () => copyText(t.url);
    div.querySelector('[data-del]').onclick = async () => {
      if (!confirm('このWebhookを削除しますか?')) return;
      await api('/api/tokens/' + t.id, { method: 'DELETE' });
      await loadWebhooks();
    };
    el.appendChild(div);
  }
}

async function createWebhook() {
  await api(`/api/channels/${state.current}/webhooks`, { method: 'POST', body: JSON.stringify({
    name: $('newWebhookName').value.trim(),
  }) });
  $('newWebhookName').value = '';
  await loadWebhooks();
  toast('Webhookを発行しました');
}

async function clearMessages() {
  if (!confirm('このチャンネルの履歴をすべて削除しますか?')) return;
  await api(`/api/channels/${state.current}/messages/clear`, { method: 'POST' });
  closeModals();
  await loadMessages();
}

// ---- モーダル ----
function openModal(id) { $(id).classList.remove('hidden'); }
function closeModals() {
  $('addChannelModal').classList.add('hidden');
  $('settingsModal').classList.add('hidden');
}

// ---- SSE(開いている画面へのリアルタイム配信) ----
// 全チャンネルのイベントを1本で受け、現在のチャンネルは即描画、他は未読バッジを増やす。
function connectSSE() {
  if (!('EventSource' in window)) return; // 非対応環境は従来通り(タップ/リロードで更新)
  if (state.es) { state.es.close(); state.es = null; }

  const es = new EventSource('/api/events', { withCredentials: true });
  state.es = es;

  es.addEventListener('ready', () => sseLog('open'));

  es.addEventListener('message', (e) => {
    let ev;
    try { ev = JSON.parse(e.data); } catch (_) { return; }
    const m = ev && ev.data;
    if (!m || !ev.channel_id) return;
    sseLog('message', ev.type, ev.channel_id);
    if (ev.channel_id === state.current) {
      appendMessage(m);
    } else {
      state.unread[ev.channel_id] = (state.unread[ev.channel_id] || 0) + 1;
      renderChannelList();
    }
  });

  // 認証切れ等で切断されたら EventSource が自動再接続する。
  // ただし 401 は再接続が無限ループになり得るので、その時はログイン画面へ。
  es.onerror = () => {
    sseLog('error', es.readyState);
    if (es.readyState === EventSource.CLOSED) {
      // サーバーが接続を閉じた(認証切れの可能性)。状態を確認して必要なら再ログイン。
      fetch('/api/me', { credentials: 'same-origin' })
        .then((r) => r.json())
        .then((me) => { if (me.auth_required && !me.authed) showLogin(); })
        .catch(() => {});
    }
    // それ以外(transient)はブラウザが自動再接続するので何もしない。
  };
}

function disconnectSSE() {
  if (state.es) { state.es.close(); state.es = null; sseLog('closed'); }
}

// sseLog はデバッグ用。?debug=1 か localStorage.debug=1 のとき console に出す。
function sseLog(...args) {
  if (state.debug) console.log('[sse]', ...args);
}

// ---- 初期化 ----
async function init() {
  const me = await (await fetch('/api/me', { credentials: 'same-origin' })).json();
  state.authRequired = me.auth_required;
  if (me.auth_required && !me.authed) { showLogin(); return; }
  showApp();
  if (me.auth_required) $('logoutBtn').classList.remove('hidden');

  await refreshNotifStatus();
  // URL の ?c= を初期選択に
  const c = new URL(location.href).searchParams.get('c');
  await loadChannels(c || null);
  connectSSE(); // 開いている画面へのリアルタイム配信を開始
}

function wire() {
  $('loginBtn').onclick = login;
  $('password').addEventListener('keydown', (e) => { if (e.key === 'Enter') login(); });
  $('logoutBtn').onclick = logout;

  $('enableBtn').onclick = enableNotifications;
  $('disableBtn').onclick = disableNotifications;
  $('testBtn').onclick = testPush;

  $('addChannelBtn').onclick = () => openModal('addChannelModal');
  $('createChannelBtn').onclick = createChannel;
  $('newChannelName').addEventListener('keydown', (e) => { if (e.key === 'Enter') createChannel(); });

  $('settingsBtn').onclick = openSettings;
  $('saveChannelBtn').onclick = saveChannel;
  $('deleteChannelBtn').onclick = deleteChannel;
  $('createWebhookBtn').onclick = createWebhook;
  $('newWebhookName').addEventListener('keydown', (e) => { if (e.key === 'Enter') createWebhook(); });
  $('clearMessagesBtn').onclick = clearMessages;

  $('menuBtn').onclick = () => $('app').classList.toggle('drawer-open');

  document.querySelectorAll('[data-close-modal]').forEach((b) => { b.onclick = closeModals; });
  document.querySelectorAll('.modal-bg').forEach((bg) => {
    bg.addEventListener('click', (e) => { if (e.target === bg) closeModals(); });
  });

  // 通知クリックで開いたとき: チャンネル切替 + 履歴更新
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.addEventListener('message', (e) => {
      const data = e.data || {};
      if (data.type === 'navigate' && data.channel) {
        loadChannels(data.channel);
      } else if (data === 'refresh' || data.type === 'refresh') {
        loadMessages();
      }
    });
  }
}

wire();
init();
