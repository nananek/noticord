'use strict';

const $ = (id) => document.getElementById(id);

function toast(msg) {
  const t = $('toast');
  t.textContent = msg;
  t.classList.add('show');
  clearTimeout(toast._t);
  toast._t = setTimeout(() => t.classList.remove('show'), 2500);
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

function urlBase64ToUint8Array(base64String) {
  const padding = '='.repeat((4 - (base64String.length % 4)) % 4);
  const base64 = (base64String + padding).replace(/-/g, '+').replace(/_/g, '/');
  const raw = atob(base64);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

function fmtTime(unix) {
  const d = new Date(unix * 1000);
  return d.toLocaleString('ja-JP', { hour12: false });
}

// ---- 画面切り替え ----

function showLogin() {
  $('loginSection').classList.remove('hidden');
  $('app').classList.add('hidden');
}
function showApp() {
  $('loginSection').classList.add('hidden');
  $('app').classList.remove('hidden');
}

// ---- 認証 ----

async function login() {
  $('loginError').textContent = '';
  const res = await fetch('/api/login', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ password: $('password').value }),
  });
  if (res.ok) {
    $('password').value = '';
    await init();
  } else {
    $('loginError').textContent = 'パスワードが違います';
  }
}

async function logout() {
  await api('/api/logout', { method: 'POST' });
  showLogin();
}

// ---- 通知 ----

let swReg = null;

async function refreshNotifStatus() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    $('notifStatus').textContent = '通知: 非対応';
    return;
  }
  swReg = await navigator.serviceWorker.getRegistration();
  const sub = swReg ? await swReg.pushManager.getSubscription() : null;
  if (sub && Notification.permission === 'granted') {
    $('notifStatus').textContent = '通知: 有効';
    $('notifStatus').className = 'status ok';
    $('enableBtn').classList.add('hidden');
    $('disableBtn').classList.remove('hidden');
  } else {
    $('notifStatus').textContent = '通知: 未設定';
    $('notifStatus').className = 'status off';
    $('enableBtn').classList.remove('hidden');
    $('disableBtn').classList.add('hidden');
  }
}

async function enableNotifications() {
  if (!('serviceWorker' in navigator) || !('PushManager' in window)) {
    toast('このブラウザはWeb Push非対応です');
    return;
  }
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') {
    toast('通知が許可されませんでした');
    return;
  }
  swReg = await navigator.serviceWorker.register('/sw.js');
  await navigator.serviceWorker.ready;

  const keyRes = await api('/api/vapid-public-key');
  const { key } = await keyRes.json();

  const sub = await swReg.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(key),
  });

  await api('/api/subscribe', { method: 'POST', body: JSON.stringify(sub.toJSON()) });
  toast('通知を有効化しました');
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
  const res = await api('/api/test', { method: 'POST' });
  const d = await res.json();
  toast(`送信: ${d.sent}/${d.subscriptions} 件`);
}

// ---- トークン ----

async function loadTokens() {
  const res = await api('/api/tokens');
  const tokens = await res.json();
  const el = $('tokens');
  el.innerHTML = '';
  if (!tokens.length) {
    el.innerHTML = '<p class="muted">まだURLがありません。</p>';
    return;
  }
  for (const t of tokens) {
    const div = document.createElement('div');
    div.className = 'token';
    const used = t.last_used_at ? `最終利用 ${fmtTime(t.last_used_at)}` : '未使用';
    div.innerHTML = `
      <div class="row">
        <strong class="grow">${escapeHtml(t.name || '(無名)')}</strong>
        <button class="danger" data-del="${t.id}">削除</button>
      </div>
      <div class="url" data-url>${escapeHtml(t.url)}</div>
      <div class="row">
        <button class="secondary" data-copy>コピー</button>
        <span class="muted">${used}</span>
      </div>`;
    div.querySelector('[data-copy]').onclick = () => copyText(t.url);
    div.querySelector('[data-del]').onclick = () => deleteToken(t.id);
    el.appendChild(div);
  }
}

async function createToken() {
  const name = $('tokenName').value.trim();
  await api('/api/tokens', { method: 'POST', body: JSON.stringify({ name }) });
  $('tokenName').value = '';
  await loadTokens();
  toast('URLを発行しました');
}

async function deleteToken(id) {
  if (!confirm('このURLを削除しますか?')) return;
  await api('/api/tokens/' + id, { method: 'DELETE' });
  await loadTokens();
}

// ---- 履歴 ----

async function loadMessages() {
  const res = await api('/api/messages?limit=100');
  const msgs = await res.json();
  const el = $('messages');
  el.innerHTML = '';
  if (!msgs.length) {
    el.innerHTML = '<p class="center muted">まだ通知はありません。</p>';
    return;
  }
  for (const m of msgs) {
    const div = document.createElement('div');
    div.className = 'msg';

    let embeds = [];
    if (m.embeds && m.embeds !== '[]') {
      try { embeds = JSON.parse(m.embeds) || []; } catch (_) {}
    }

    const head = `
      <div class="row">
        <span class="title grow">${escapeHtml(m.username || 'noticord')}</span>
        <span class="time">${fmtTime(m.created_at)}</span>
        <button class="danger" data-del="${m.id}">×</button>
      </div>`;
    const content = m.content ? `<div class="body">${markdown(m.content)}</div>` : '';
    const embedHtml = embeds.map(renderEmbed).join('');

    div.innerHTML = head + content + embedHtml;
    div.querySelector('[data-del]').onclick = async () => {
      await api('/api/messages/' + m.id, { method: 'DELETE' });
      await loadMessages();
    };
    el.appendChild(div);
  }
}

// Discord の embed を1枚のカードとしてレンダリングする。
function renderEmbed(e) {
  if (!e || typeof e !== 'object') return '';
  const color = typeof e.color === 'number' && e.color > 0
    ? '#' + e.color.toString(16).padStart(6, '0')
    : '#4f545c';

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

  if (e.description) {
    inner += `<div class="embed-desc">${markdown(e.description)}</div>`;
  }

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

  if (e.footer && e.footer.text || e.timestamp) {
    const icon = e.footer && safeUrl(e.footer.icon_url)
      ? `<img class="embed-footer-icon" src="${escapeAttr(e.footer.icon_url)}" alt="">` : '';
    const txt = e.footer && e.footer.text ? escapeHtml(e.footer.text) : '';
    const ts = e.timestamp ? `<span class="embed-ts">${escapeHtml(fmtIso(e.timestamp))}</span>` : '';
    const sep = txt && ts ? ' • ' : '';
    inner += `<div class="embed-footer">${icon}<span>${txt}${sep}</span>${ts}</div>`;
  }

  const body = thumb
    ? `<div class="embed-grid"><div class="embed-main">${inner}</div>${thumb}</div>`
    : inner;

  return `<div class="embed" style="border-left-color:${color}">${body}</div>`;
}

function fmtIso(s) {
  const d = new Date(s);
  return isNaN(d) ? s : d.toLocaleString('ja-JP', { hour12: false });
}

// http/https のみ許可。それ以外(javascript: 等)は弾く。
function safeUrl(u) {
  if (!u || typeof u !== 'string') return false;
  return /^https?:\/\//i.test(u.trim());
}

function escapeAttr(s) { return escapeHtml(s); }

// 行をまたぐ簡易 Markdown。改行を <br> に。
function markdown(s) {
  return markdownInline(String(s)).replace(/\n/g, '<br>');
}

// インライン Markdown: **太字** *斜体* __下線__ ~~取消~~ `コード` [text](url)
// まず HTML エスケープしてから安全な置換のみ行う。
function markdownInline(s) {
  let t = escapeHtml(String(s));
  // リンク [text](http...)
  t = t.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g,
    (_, text, url) => `<a href="${url}" target="_blank" rel="noopener">${text}</a>`);
  t = t.replace(/`([^`]+)`/g, '<code>$1</code>');
  t = t.replace(/\*\*([^*]+)\*\*/g, '<strong>$1</strong>');
  t = t.replace(/__([^_]+)__/g, '<u>$1</u>');
  t = t.replace(/~~([^~]+)~~/g, '<s>$1</s>');
  t = t.replace(/\*([^*]+)\*/g, '<em>$1</em>');
  return t;
}

async function clearMessages() {
  if (!confirm('受信履歴をすべて削除しますか?')) return;
  await api('/api/messages/clear', { method: 'POST' });
  await loadMessages();
}

// ---- ユーティリティ ----

function escapeHtml(s) {
  return String(s).replace(/[&<>"']/g, (c) => ({
    '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;',
  }[c]));
}

async function copyText(text) {
  try {
    await navigator.clipboard.writeText(text);
    toast('コピーしました');
  } catch (_) {
    toast('コピーできませんでした');
  }
}

// ---- 初期化 ----

async function init() {
  const me = await (await fetch('/api/me', { credentials: 'same-origin' })).json();
  if (me.auth_required && !me.authed) {
    showLogin();
    return;
  }
  showApp();
  if (me.auth_required) $('logoutBtn').classList.remove('hidden');
  await refreshNotifStatus();
  await loadTokens();
  await loadMessages();
}

function wire() {
  $('loginBtn').onclick = login;
  $('password').addEventListener('keydown', (e) => { if (e.key === 'Enter') login(); });
  $('enableBtn').onclick = enableNotifications;
  $('disableBtn').onclick = disableNotifications;
  $('testBtn').onclick = testPush;
  $('createTokenBtn').onclick = createToken;
  $('tokenName').addEventListener('keydown', (e) => { if (e.key === 'Enter') createToken(); });
  $('refreshBtn').onclick = loadMessages;
  $('clearBtn').onclick = clearMessages;
  $('logoutBtn').onclick = logout;

  // SW から「通知クリックで開いた」通知を受けたら履歴を更新
  if ('serviceWorker' in navigator) {
    navigator.serviceWorker.addEventListener('message', (e) => {
      if (e.data === 'refresh') loadMessages();
    });
  }
}

wire();
init();
