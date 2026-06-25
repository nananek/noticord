'use strict';

const $ = (id) => document.getElementById(id);

// ---- i18n ----
// サーバー(/api/me の lang)が選んだ言語で UI を切り替える。対応は en/ja のみ、既定 en。
// 文言の一部は {name} 等のプレースホルダを持ち、tr(key, {name}) で差し込む。
const I18N = {
  en: {
    'login.password_ph': 'Password',
    'login.button': 'Log in',
    'login.wrong': 'Wrong password',
    'notif.label': 'Alerts',
    'notif.enable': 'Enable notifications',
    'notif.sound': 'Notification sound',
    'notif.test': 'Send a test notification',
    'notif.disable': 'Disable notifications',
    'notif.settings': 'Notifications',
    'notif.test_label': 'Test',
    'notif.unsupported': 'Unsupported',
    'notif.on': 'Alerts ON',
    'notif.off': 'Alerts OFF',
    'logout': 'Log out',
    'sidebar.channels': 'Channels',
    'sidebar.add_channel': 'Add channel',
    'sidebar.add_group': 'Add group',
    'group.new_prompt': 'New group name',
    'group.rename_prompt': 'Group name',
    'group.menu': 'Rename / delete',
    'group.none': '(no group)',
    'confirm.delete_group': 'Delete the group "{name}"?\nIts channels will be moved out of the group (not deleted).',
    'toast.group_created': 'Group created',
    'toast.group_deleted': 'Group deleted',
    'header.menu': 'Channels',
    'header.settings': 'Channel settings',
    'timeline.select_channel': 'Select a channel',
    'timeline.no_channels': 'Create a channel with the "+" button',
    'addch.title': 'Add channel',
    'addch.name_label': 'Channel name (lowercased; symbols become -)',
    'addch.topic_label': 'Topic (optional)',
    'addch.topic_ph': 'Server monitoring alerts',
    'common.cancel': 'Cancel',
    'common.create': 'Create',
    'common.close': 'Close',
    'common.save': 'Save',
    'common.delete': 'Delete',
    'common.copy': 'Copy',
    'created.heading': 'Created #{name}',
    'created.desc': "A Webhook URL for this channel has been issued. Paste it into the Discord webhook target of the sending service.",
    'created.copy': 'Copy URL',
    'settings.heading': 'Channel settings: #{name}',
    'settings.name_label': 'Channel name',
    'settings.topic_label': 'Topic',
    'settings.group_label': 'Group',
    'settings.notify_label': 'Push notifications',
    'settings.sound_label': 'Notification sound',
    'settings.badge_label': 'Unread badge',
    'settings.delete_channel': 'Delete channel',
    'settings.webhook_desc': 'A Discord-compatible URL that delivers to this channel. Paste it into the webhook target of the sending service.',
    'settings.webhook_name_ph': 'Purpose (e.g. Grafana)',
    'settings.webhook_create': 'Issue',
    'settings.clear_history': 'Delete all history in this channel',
    'settings.no_channel': 'No channel selected',
    'toast.copied': 'Copied',
    'toast.copy_failed': 'Could not copy',
    'toast.add_to_home': 'First add to the Home Screen, then open from that icon',
    'toast.webpush_unsupported': 'This browser does not support Web Push',
    'toast.notif_blocked': 'Notifications are blocked. Allow them in your device settings',
    'toast.notif_denied': 'Notification permission was not granted',
    'toast.notif_enabled': 'Notifications enabled',
    'toast.subscribe_failed': 'Subscription failed: {msg}',
    'toast.notif_disabled': 'Notifications disabled',
    'toast.reorder_failed': 'Could not save the new order',
    'toast.name_required': 'Enter a name',
    'toast.create_failed': 'Could not create',
    'toast.channel_created': 'Created #{name}',
    'toast.save_failed': 'Could not save',
    'toast.saved': 'Saved',
    'toast.delete_failed': 'Could not delete',
    'toast.channel_deleted': 'Channel deleted',
    'toast.webhook_created': 'Webhook issued',
    'push.guide_ios_pwa': '📲 On iPhone / iPad, use the share menu’s "Add to Home Screen", <b>launch from that icon</b>, and then enable notifications. They will not fire in a Safari tab.',
    'push.guide_ios_old': '📲 This device cannot receive notifications. Update to iOS 16.4+ and launch from the icon added to the Home Screen.',
    'push.guide_unsupported': 'This browser does not support Web Push.',
    'push.sent': 'Sent: {sent}/{subs}{detail}',
    'push.expired': 'expired ({status})',
    'push.dev_apple': 'iPhone (apple)',
    'push.dev_fcm': 'Android/Chrome (fcm)',
    'push.dev_unknown': 'device',
    'grip.title': 'Drag to reorder',
    'msg.none': "No notifications yet.<br>Send to this channel's Webhook URL and they will show up here.",
    'msg.delete': 'Delete',
    'webhook.none': 'No webhooks yet.',
    'webhook.last_used': 'Last used {time}',
    'webhook.unused': 'Unused',
    'webhook.noname': '(unnamed)',
    'confirm.delete_channel': 'Delete #{name}?\nIts webhooks and history will all be deleted.',
    'confirm.delete_webhook': 'Delete this webhook?',
    'confirm.clear_history': 'Delete all history in this channel?',
  },
  ja: {
    'login.password_ph': 'パスワード',
    'login.button': 'ログイン',
    'login.wrong': 'パスワードが違います',
    'notif.label': '通知',
    'notif.enable': '通知を有効化',
    'notif.sound': '通知音',
    'notif.test': 'テスト通知を送信',
    'notif.disable': '通知を無効化',
    'notif.settings': '通知設定',
    'notif.test_label': 'テスト',
    'notif.unsupported': '非対応',
    'notif.on': '通知 ON',
    'notif.off': '通知 OFF',
    'logout': 'ログアウト',
    'sidebar.channels': 'チャンネル',
    'sidebar.add_channel': 'チャンネルを追加',
    'sidebar.add_group': 'グループを追加',
    'group.new_prompt': '新しいグループ名',
    'group.rename_prompt': 'グループ名',
    'group.menu': '名前変更 / 削除',
    'group.none': '(グループなし)',
    'confirm.delete_group': 'グループ「{name}」を削除しますか?\n所属チャンネルはグループ解除されます(削除はされません)。',
    'toast.group_created': 'グループを作成しました',
    'toast.group_deleted': 'グループを削除しました',
    'header.menu': 'チャンネル',
    'header.settings': 'チャンネル設定',
    'timeline.select_channel': 'チャンネルを選択してください',
    'timeline.no_channels': '「＋」からチャンネルを作成してください',
    'addch.title': 'チャンネルを追加',
    'addch.name_label': 'チャンネル名(小文字・記号は - に変換されます)',
    'addch.topic_label': 'トピック(任意)',
    'addch.topic_ph': 'サーバー監視アラート',
    'common.cancel': 'キャンセル',
    'common.create': '作成',
    'common.close': '閉じる',
    'common.save': '保存',
    'common.delete': '削除',
    'common.copy': 'コピー',
    'created.heading': '#{name} を作成しました',
    'created.desc': 'このチャンネル宛の Webhook URL を発行しました。送信元サービスの Discord Webhook 先に貼り付けてください。',
    'created.copy': 'URL をコピー',
    'settings.heading': 'チャンネル設定: #{name}',
    'settings.name_label': 'チャンネル名',
    'settings.topic_label': 'トピック',
    'settings.group_label': 'グループ',
    'settings.notify_label': 'プッシュ通知',
    'settings.sound_label': '通知音',
    'settings.badge_label': '未読バッジ',
    'settings.delete_channel': 'チャンネルを削除',
    'settings.webhook_desc': 'このチャンネルに届くDiscord互換URLです。送信元サービスのWebhook先に貼り付けてください。',
    'settings.webhook_name_ph': '用途名 (例: Grafana)',
    'settings.webhook_create': '発行',
    'settings.clear_history': 'このチャンネルの履歴を全削除',
    'settings.no_channel': 'チャンネルがありません',
    'toast.copied': 'コピーしました',
    'toast.copy_failed': 'コピーできませんでした',
    'toast.add_to_home': 'まず「ホーム画面に追加」して、そのアイコンから開いてください',
    'toast.webpush_unsupported': 'このブラウザはWeb Push非対応です',
    'toast.notif_blocked': '通知がブロックされています。端末の設定から許可してください',
    'toast.notif_denied': '通知が許可されませんでした',
    'toast.notif_enabled': '通知を有効化しました',
    'toast.subscribe_failed': '購読に失敗しました: {msg}',
    'toast.notif_disabled': '通知を無効化しました',
    'toast.reorder_failed': '並び替えの保存に失敗しました',
    'toast.name_required': '名前を入力してください',
    'toast.create_failed': '作成失敗',
    'toast.channel_created': '#{name} を作成しました',
    'toast.save_failed': '保存失敗',
    'toast.saved': '保存しました',
    'toast.delete_failed': '削除失敗',
    'toast.channel_deleted': 'チャンネルを削除しました',
    'toast.webhook_created': 'Webhookを発行しました',
    'push.guide_ios_pwa': '📲 iPhone / iPad では、共有メニューの「ホーム画面に追加」で追加し、その<b>アイコンから起動</b>してから通知を有効化してください。Safari のタブでは通知が鳴りません。',
    'push.guide_ios_old': '📲 この端末では通知に未対応です。iOS 16.4 以上にして、ホーム画面に追加したアイコンから起動してください。',
    'push.guide_unsupported': 'このブラウザは Web Push に対応していません。',
    'push.sent': '送信: {sent}/{subs} 件{detail}',
    'push.expired': '失効({status})',
    'push.dev_apple': 'iPhone(apple)',
    'push.dev_fcm': 'Android/Chrome(fcm)',
    'push.dev_unknown': 'device',
    'grip.title': 'ドラッグで並び替え',
    'msg.none': 'まだ通知はありません。<br>このチャンネルのWebhook URLに送ると、ここに表示されます。',
    'msg.delete': '削除',
    'webhook.none': 'まだWebhookがありません。',
    'webhook.last_used': '最終利用 {time}',
    'webhook.unused': '未使用',
    'webhook.noname': '(無名)',
    'confirm.delete_channel': '#{name} を削除しますか?\nWebhookと履歴もすべて削除されます。',
    'confirm.delete_webhook': 'このWebhookを削除しますか?',
    'confirm.clear_history': 'このチャンネルの履歴をすべて削除しますか?',
  },
};

let LANG = 'en';

// tr はキーを現在の言語へ訳す。未知キーは en へフォールバックし、なお無ければキー自身を返す。
// params があれば {name} 形式のプレースホルダを置換する。
function tr(key, params) {
  const table = I18N[LANG] || I18N.en;
  let s = table[key];
  if (s == null) s = I18N.en[key];
  if (s == null) s = key;
  if (params) for (const k in params) s = s.split('{' + k + '}').join(params[k]);
  return s;
}

// applyI18n は lang を確定し、静的 DOM の data-i18n 系属性をまとめて訳語に差し替える。
function applyI18n(lang) {
  LANG = (lang === 'ja') ? 'ja' : 'en';
  document.documentElement.lang = LANG;
  document.querySelectorAll('[data-i18n]').forEach((el) => { el.textContent = tr(el.getAttribute('data-i18n')); });
  document.querySelectorAll('[data-i18n-ph]').forEach((el) => { el.placeholder = tr(el.getAttribute('data-i18n-ph')); });
  document.querySelectorAll('[data-i18n-title]').forEach((el) => { el.title = tr(el.getAttribute('data-i18n-title')); });
}

// 日付表示用ロケール(言語に追従)。
function dateLocale() { return LANG === 'ja' ? 'ja-JP' : 'en-US'; }

// ---- 状態 ----
const state = {
  channels: [],
  groups: [],      // チャンネルグループ(カテゴリ)
  current: null,   // 選択中チャンネル id
  authRequired: false,
  unread: {},      // channelId -> 未読件数
  // channelId -> 既読の最大メッセージ id(Discord 風「new messages」仕切り用)。
  lastRead: loadLastRead(),
  es: null,        // EventSource
  // 音通知。既定 ON。localStorage で永続化。
  sound: (typeof localStorage === 'undefined' || localStorage.getItem('sound') !== '0'),
  // ?debug=1 か localStorage.debug=1 でクライアント側 SSE ログを有効化。
  debug: new URL(location.href).searchParams.get('debug') === '1' ||
    (typeof localStorage !== 'undefined' && localStorage.getItem('debug') === '1'),
};

// 既読位置(channelId -> 最大既読メッセージ id)を localStorage で永続化する。
function loadLastRead() {
  try { return JSON.parse(localStorage.getItem('lastRead') || '{}') || {}; }
  catch (_) { return {}; }
}
function saveLastRead() {
  try { localStorage.setItem('lastRead', JSON.stringify(state.lastRead)); } catch (_) {}
}

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
  return new Date(unix * 1000).toLocaleString(dateLocale(), { hour12: false });
}
function fmtIso(s) {
  const d = new Date(s);
  return isNaN(d) ? s : d.toLocaleString(dateLocale(), { hour12: false });
}

// Discord タイムスタンプ <t:UNIX:STYLE> のスタイル別フォーマット。
// STYLE: t=短時刻 T=長時刻 d=短日付 D=長日付 f=日付+時刻(既定) F=曜日+日付+時刻 R=相対。
const TS_STYLE_OPTS = {
  t: { timeStyle: 'short' },
  T: { timeStyle: 'medium' },
  d: { dateStyle: 'short' },
  D: { dateStyle: 'long' },
  f: { dateStyle: 'long', timeStyle: 'short' },
  F: { dateStyle: 'full', timeStyle: 'short' },
};
function relTime(unix) {
  const diff = unix - Math.floor(Date.now() / 1000);
  const abs = Math.abs(diff);
  const rtf = new Intl.RelativeTimeFormat(dateLocale(), { numeric: 'auto' });
  const units = [['year', 31536000], ['month', 2592000], ['week', 604800],
    ['day', 86400], ['hour', 3600], ['minute', 60], ['second', 1]];
  for (const [unit, secs] of units) {
    if (abs >= secs || unit === 'second') return rtf.format(Math.round(diff / secs), unit);
  }
}
// 表示文字列とツールチップ(常に完全表記)を返す。不正な値なら null。
function fmtDiscordTs(unix, style) {
  const n = Number(unix);
  if (!Number.isFinite(n)) return null;
  const d = new Date(n * 1000);
  if (isNaN(d)) return null;
  const loc = dateLocale();
  const s = TS_STYLE_OPTS[style] ? style : 'f';
  const text = style === 'R' ? relTime(n)
    : d.toLocaleString(loc, { hour12: false, ...TS_STYLE_OPTS[s] });
  const title = d.toLocaleString(loc, { hour12: false, dateStyle: 'full', timeStyle: 'short' });
  return { text, title };
}
// 相対表記(:R)は時間経過で古くなるので定期的に貼り替える。
function tickRelativeTimestamps() {
  document.querySelectorAll('.md-ts[data-rel]').forEach((el) => {
    const r = fmtDiscordTs(el.getAttribute('data-ts'), 'R');
    if (r) el.textContent = r.text;
  });
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
  try { await navigator.clipboard.writeText(text); toast(tr('toast.copied')); }
  catch (_) { toast(tr('toast.copy_failed')); }
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
  // Discord タイムスタンプ。escapeHtml 済みなので < > は &lt; &gt; で来る。最後に処理する。
  t = t.replace(/&lt;t:(\d{1,15})(?::([tTdDfFR]))?&gt;/g, (m, unix, style) => {
    const r = fmtDiscordTs(unix, style);
    if (!r) return m;
    const rel = style === 'R' ? ` data-ts="${unix}" data-rel="1"` : '';
    return `<span class="md-ts"${rel} title="${escapeAttr(r.title)}">${escapeHtml(r.text)}</span>`;
  });
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
  else { $('loginError').textContent = tr('login.wrong'); }
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
    msg = tr('push.guide_ios_pwa');
  } else if (!pushSupported()) {
    if (isIOS()) {
      msg = tr('push.guide_ios_old');
    } else {
      msg = tr('push.guide_unsupported');
    }
  }
  if (msg) { el.innerHTML = msg; el.classList.remove('hidden'); }
  else el.classList.add('hidden');
}

// ブランドバーと通知設定シートのステータスバッジを揃えて更新する。
function setNotifStatus(textKey, cls) {
  for (const id of ['notifDot', 'notifDotSheet']) {
    const el = $(id);
    if (el) { el.textContent = tr(textKey); el.className = 'status ' + cls; }
  }
}

async function refreshNotifStatus() {
  updateGuidance();
  if (!pushSupported()) {
    setNotifStatus('notif.unsupported', 'off');
    // iOS タブ時は「有効化」を押させて誘導を出したいので disabled にはしない。
    $('enableBtn').disabled = !isIOS();
    return;
  }
  $('enableBtn').disabled = false;
  swReg = await navigator.serviceWorker.getRegistration();
  const sub = swReg ? await swReg.pushManager.getSubscription() : null;
  if (sub && Notification.permission === 'granted') {
    setNotifStatus('notif.on', 'ok');
    $('enableBtn').classList.add('hidden');
    $('disableBtn').classList.remove('hidden');
  } else {
    setNotifStatus('notif.off', 'off');
    $('enableBtn').classList.remove('hidden');
    $('disableBtn').classList.add('hidden');
  }
}

async function enableNotifications() {
  // iOS でタブ起動だと PushManager が無く、購読しても鳴らない。先に誘導する。
  if (isIOS() && !isStandalone()) {
    updateGuidance();
    toast(tr('toast.add_to_home'));
    return;
  }
  if (!pushSupported()) {
    toast(tr('toast.webpush_unsupported'));
    updateGuidance();
    return;
  }
  const perm = await Notification.requestPermission();
  if (perm !== 'granted') {
    toast(perm === 'denied' ? tr('toast.notif_blocked') : tr('toast.notif_denied'));
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
    toast(tr('toast.notif_enabled'));
  } catch (e) {
    toast(tr('toast.subscribe_failed', { msg: (e && e.message ? e.message : e) }));
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
  toast(tr('toast.notif_disabled'));
  await refreshNotifStatus();
}

async function testPush() {
  const d = await (await api('/api/test', { method: 'POST' })).json();
  // デバイスごとの結果(apple/fcm + ステータス)を要約して見せる。
  let detail = '';
  if (Array.isArray(d.results) && d.results.length) {
    detail = '\n' + d.results.map((r) => {
      const where = (r.host || '').includes('apple') ? tr('push.dev_apple')
        : (r.host || '').includes('google') ? tr('push.dev_fcm')
        : (r.host || tr('push.dev_unknown'));
      const st = r.error ? `ERR ${r.error}` : (r.pruned ? tr('push.expired', { status: r.status }) : `OK ${r.status}`);
      return `・${where}: ${st}`;
    }).join('\n');
  }
  toast(tr('push.sent', { sent: d.sent, subs: d.subscriptions, detail }));
}

// ---- チャンネル ----
async function loadChannels(selectId) {
  // チャンネルとグループをまとめて取得(描画はグループ単位)。
  const [chs, grs] = await Promise.all([
    (await api('/api/channels')).json(),
    (await api('/api/groups')).json(),
  ]);
  state.channels = chs;
  state.groups = grs;
  renderChannelList();
  // 選択チャンネルを決める: 指定 > 既存維持 > 先頭
  let target = selectId || state.current;
  if (!state.channels.find((c) => c.id === target)) {
    target = state.channels.length ? state.channels[0].id : null;
  }
  if (target) await selectChannel(target);
  else renderEmptyMain();
}

// channelItem は1チャンネル分の DOM を作る。ミュート表示・未読バッジは
// チャンネル別設定(badge / notify)に従う。
function channelItem(c) {
  const div = document.createElement('div');
  div.className = 'channel-item' + (c.id === state.current ? ' active' : '') + (c.notify ? '' : ' muted');
  div.draggable = true;
  div.dataset.id = c.id;
  const n = state.unread[c.id] || 0;
  // 未読バッジは badge=ON のときだけ表示する(Discord のミュート相当)。
  const badge = (c.badge && n > 0) ? `<span class="badge">${n > 99 ? '99+' : n}</span>` : '';
  const mute = c.notify ? '' : '<span class="mute-ico">🔕</span>';
  div.innerHTML = `
    <span class="grip" title="${escapeAttr(tr('grip.title'))}">⠿</span>
    <span class="hash">#</span>
    <span class="cname">${escapeHtml(c.name)}</span>${badge}${mute}`;
  div.onclick = () => { selectChannel(c.id); $('app').classList.remove('drawer-open'); };
  wireChannelDrag(div);
  return div;
}

function renderChannelList() {
  const el = $('channelList');
  el.innerHTML = '';
  // 未所属チャンネルを先頭に並べる。
  for (const c of state.channels) {
    if (!c.group_id) el.appendChild(channelItem(c));
  }
  // グループは position 順。各グループの見出し + 配下チャンネル。
  const groups = state.groups.slice().sort((a, b) => a.position - b.position);
  for (const g of groups) {
    const wrap = document.createElement('div');
    wrap.className = 'group';
    wrap.dataset.gid = g.id;

    // 折りたたみ時は配下チャンネルの未読を見出しに合算表示する(badge=ON のみ)。
    let groupUnread = 0;
    for (const c of state.channels) {
      if (c.group_id === g.id && c.badge) groupUnread += state.unread[c.id] || 0;
    }
    const gBadge = (g.collapsed && groupUnread > 0)
      ? `<span class="badge gbadge">${groupUnread > 99 ? '99+' : groupUnread}</span>` : '';

    const head = document.createElement('div');
    head.className = 'group-head' + (g.collapsed ? ' collapsed' : '');
    head.dataset.gid = g.id;
    head.draggable = true;
    head.innerHTML = `
      <span class="chevron">▾</span>
      <span class="ghandle" title="${escapeAttr(tr('grip.title'))}">⠿</span>
      <span class="gname">${escapeHtml(g.name)}</span>${gBadge}
      <button class="gmenu grename" title="${escapeAttr(tr('group.rename_prompt'))}">✎</button>
      <button class="gmenu gdelete" title="${escapeAttr(tr('common.delete'))}">🗑</button>`;
    head.onclick = (e) => {
      if (e.target.closest('.gmenu') || e.target.closest('.ghandle')) return;
      toggleGroupCollapse(g);
    };
    head.querySelector('.grename').onclick = (e) => { e.stopPropagation(); renameGroup(g); };
    head.querySelector('.gdelete').onclick = (e) => { e.stopPropagation(); deleteGroup(g); };

    const kids = document.createElement('div');
    kids.className = 'group-children' + (g.collapsed ? ' collapsed' : '');
    for (const c of state.channels) {
      if (c.group_id === g.id) kids.appendChild(channelItem(c));
    }
    wireGroupHead(head);
    wrap.appendChild(head);
    wrap.appendChild(kids);
    el.appendChild(wrap);
  }
}

// ---- チャンネル並び替え(ドラッグ&ドロップ) ----
// dragKind は 'channel' か 'group'。dragId は対象 id。
let dragKind = null;
let dragId = null;

function clearDropMarks() {
  document.querySelectorAll('.channel-item, .group-head').forEach((x) =>
    x.classList.remove('dragging', 'drop-before', 'drop-after', 'drop-into'));
}

function wireChannelDrag(el) {
  el.addEventListener('dragstart', (e) => {
    e.stopPropagation();
    dragKind = 'channel';
    dragId = el.dataset.id;
    el.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    try { e.dataTransfer.setData('text/plain', dragId); } catch (_) {}
  });
  el.addEventListener('dragend', () => { dragKind = null; dragId = null; clearDropMarks(); });
  el.addEventListener('dragover', (e) => {
    if (dragKind !== 'channel') return;
    e.preventDefault();
    if (!dragId || el.dataset.id === dragId) return;
    const before = e.offsetY < el.offsetHeight / 2;
    el.classList.toggle('drop-before', before);
    el.classList.toggle('drop-after', !before);
  });
  el.addEventListener('dragleave', () => el.classList.remove('drop-before', 'drop-after'));
  el.addEventListener('drop', (e) => {
    if (dragKind !== 'channel') return;
    e.preventDefault();
    e.stopPropagation();
    const before = el.classList.contains('drop-before');
    el.classList.remove('drop-before', 'drop-after');
    if (dragId && el.dataset.id !== dragId) {
      const dst = state.channels.find((c) => c.id === el.dataset.id);
      reorderChannel(dragId, dst ? (dst.group_id || '') : '', el.dataset.id, before);
    }
  });
}

// グループ見出しのドラッグ: 自身はグループ並び替え、チャンネルが乗ったら
// そのグループへ取り込む(drop-into)。
function wireGroupHead(head) {
  head.addEventListener('dragstart', (e) => {
    dragKind = 'group';
    dragId = head.dataset.gid;
    head.classList.add('dragging');
    e.dataTransfer.effectAllowed = 'move';
    try { e.dataTransfer.setData('text/plain', dragId); } catch (_) {}
  });
  head.addEventListener('dragend', () => { dragKind = null; dragId = null; clearDropMarks(); });
  head.addEventListener('dragover', (e) => {
    e.preventDefault();
    if (dragKind === 'channel') head.classList.add('drop-into');
    else if (dragKind === 'group' && head.dataset.gid !== dragId) head.classList.add('drop-before');
  });
  head.addEventListener('dragleave', () => head.classList.remove('drop-into', 'drop-before'));
  head.addEventListener('drop', (e) => {
    e.preventDefault();
    e.stopPropagation();
    head.classList.remove('drop-into', 'drop-before');
    if (dragKind === 'channel' && dragId) {
      reorderChannel(dragId, head.dataset.gid, null, false); // グループ末尾へ取り込む
    } else if (dragKind === 'group' && dragId && head.dataset.gid !== dragId) {
      reorderGroup(dragId, head.dataset.gid);
    }
  });
}

// reorderChannel は src を dstGroupId 内の dst の前/後へ移動する(楽観的更新)。
// dstId が null ならそのグループの末尾へ。group_id も合わせて更新する。
async function reorderChannel(srcId, dstGroupId, dstId, before) {
  const arr = state.channels;
  const src = arr.find((c) => c.id === srcId);
  if (!src) return;
  src.group_id = dstGroupId || '';
  arr.splice(arr.indexOf(src), 1); // 元位置から抜く
  let to;
  if (dstId) {
    const dst = arr.find((c) => c.id === dstId);
    to = arr.indexOf(dst);
    if (to < 0) return;
    if (!before) to += 1;
  } else {
    // グループ見出しへのドロップ: そのグループの末尾(配列上の最後)へ。
    let last = -1;
    arr.forEach((c, i) => { if ((c.group_id || '') === (dstGroupId || '')) last = i; });
    to = last >= 0 ? last + 1 : arr.length;
  }
  arr.splice(to, 0, src);
  renderChannelList();
  await persistLayout();
}

// persistLayout は現在の配列順と所属グループをサーバーへ保存する。
async function persistLayout() {
  const layout = state.channels.map((c) => ({ id: c.id, group_id: c.group_id || '' }));
  try {
    await api('/api/channels/reorder', { method: 'POST', body: JSON.stringify({ layout }) });
  } catch (_) {
    toast(tr('toast.reorder_failed'));
    await loadChannels(state.current);
  }
}

// ---- グループ操作 ----
async function addGroup() {
  const name = prompt(tr('group.new_prompt'));
  if (name == null || !name.trim()) return;
  const res = await api('/api/groups', { method: 'POST', body: JSON.stringify({ name: name.trim() }) });
  if (!res.ok) { const e = await res.json(); toast(e.error || tr('toast.create_failed')); return; }
  await loadChannels(state.current);
  toast(tr('toast.group_created'));
}

async function renameGroup(g) {
  const name = prompt(tr('group.rename_prompt'), g.name);
  if (name == null || !name.trim() || name.trim() === g.name) return;
  await api('/api/groups/' + g.id, { method: 'PATCH', body: JSON.stringify({ name: name.trim() }) });
  await loadChannels(state.current);
}

async function deleteGroup(g) {
  if (!confirm(tr('confirm.delete_group', { name: g.name }))) return;
  await api('/api/groups/' + g.id, { method: 'DELETE' });
  await loadChannels(state.current);
  toast(tr('toast.group_deleted'));
}

async function toggleGroupCollapse(g) {
  g.collapsed = !g.collapsed; // 楽観的更新
  renderChannelList();
  await api('/api/groups/' + g.id, { method: 'PATCH', body: JSON.stringify({ collapsed: g.collapsed }) });
}

async function reorderGroup(srcId, dstId) {
  const ids = state.groups.slice().sort((a, b) => a.position - b.position).map((g) => g.id);
  const from = ids.indexOf(srcId);
  if (from < 0) return;
  ids.splice(from, 1);
  let to = ids.indexOf(dstId);
  if (to < 0) return;
  ids.splice(to, 0, srcId); // 見出しの前へ挿入
  ids.forEach((id, i) => { const g = state.groups.find((x) => x.id === id); if (g) g.position = i; });
  renderChannelList();
  try {
    await api('/api/groups/reorder', { method: 'POST', body: JSON.stringify({ order: ids }) });
  } catch (_) {
    toast(tr('toast.reorder_failed'));
    await loadChannels(state.current);
  }
}

function renderEmptyMain() {
  $('curChannelName').textContent = '—';
  $('curTopic').textContent = '';
  $('timeline').innerHTML = `<p class="center">${escapeHtml(tr('timeline.no_channels'))}</p>`;
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
  if (!name.trim()) { toast(tr('toast.name_required')); return; }
  const res = await api('/api/channels', { method: 'POST', body: JSON.stringify({
    name, topic: $('newChannelTopic').value,
  }) });
  if (!res.ok) { const e = await res.json(); toast(e.error || tr('toast.create_failed')); return; }
  const { channel: c, webhook } = await res.json();
  $('newChannelName').value = ''; $('newChannelTopic').value = '';
  closeModals();
  await loadChannels(c.id);
  // Webhook 専用コンセプト: 自動発行された URL をその場で提示して即コピーできるように。
  if (webhook && webhook.url) {
    $('createdTitle').textContent = tr('created.heading', { name: c.name });
    $('createdUrl').textContent = webhook.url;
    $('createdCopyBtn').onclick = () => copyText(webhook.url);
    openModal('createdModal');
  } else {
    toast(tr('toast.channel_created', { name: c.name }));
  }
}

// ---- メッセージ ----
function newDivider() {
  const d = document.createElement('div');
  d.className = 'new-divider';
  d.id = 'newDivider';
  d.textContent = 'new messages';
  return d;
}

async function loadMessages() {
  if (!state.current) return;
  const cid = state.current;
  const msgs = await (await api(`/api/channels/${cid}/messages?limit=100`)).json();
  const el = $('timeline');
  el.innerHTML = '';
  if (!msgs.length) {
    el.innerHTML = `<p class="center">${tr('msg.none')}</p>`;
    state.lastRead[cid] = 0; saveLastRead();
    return;
  }
  // 古い→新しいの順に縦に並べる(チャットらしく)
  msgs.reverse();

  // Discord 風「new messages」仕切り: 開いた時点の既読 id を基準に、
  // それより新しい最初のメッセージの直前へ1本だけ入れる。
  const seen = state.lastRead[cid] || 0;
  let dividerShown = false;
  for (const m of msgs) {
    if (!dividerShown && seen > 0 && m.id > seen) {
      el.appendChild(newDivider());
      dividerShown = true;
    }
    el.appendChild(renderMessage(m));
  }

  // 既読位置を最新まで進める(次に開いたときは今回分は「既読」扱い)。
  state.lastRead[cid] = msgs[msgs.length - 1].id;
  saveLastRead();

  // 仕切りがあればそこへ、無ければ最下部へスクロール。
  const dv = $('newDivider');
  if (dv) dv.scrollIntoView({ block: 'center' });
  else el.scrollTop = el.scrollHeight;
}

// appendMessage は SSE で届いた1件を現在のタイムライン末尾に追記する。
// 一番下付近を見ているときだけ自動スクロールする(上を読んでいる最中の妨げ防止)。
function appendMessage(m) {
  const el = $('timeline');
  // 空状態プレースホルダを除去。
  const ph = el.querySelector('.center');
  if (ph) el.innerHTML = '';
  const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 80;

  // タブが非表示の間に来た最初の1件には「new messages」仕切りを出す
  // (見ていない=未読扱い)。表示中ならライブ追記として既読を進める。
  if (document.hidden && !$('newDivider')) {
    el.appendChild(newDivider());
  }
  el.appendChild(renderMessage(m));

  if (!document.hidden) {
    state.lastRead[state.current] = m.id;
    saveLastRead();
  }
  if (nearBottom) el.scrollTop = el.scrollHeight;
}

// onLiveUpdate は編集(Discord の PATCH 相当)を現在のタイムラインにその場反映する。
// 行は削除ボタンの data-del(=message_id)から特定する。
function onLiveUpdate(m) {
  if (!m || m.id == null) return;
  const btn = document.querySelector('.message-row [data-del="' + m.id + '"]');
  const old = btn && btn.closest('.message-row');
  if (!old) return;
  const row = renderMessage(m);
  old.replaceWith(row);
  // 編集箇所が分かるよう一瞬ハイライト(CSS 非依存)。
  row.style.transition = 'background-color 1.2s ease';
  row.style.backgroundColor = 'rgba(88,101,242,0.18)';
  requestAnimationFrame(() => { row.style.backgroundColor = ''; });
}

// onLiveDelete は削除を現在のタイムラインから取り除く。
function onLiveDelete(id) {
  if (id == null) return;
  const btn = document.querySelector('.message-row [data-del="' + id + '"]');
  const old = btn && btn.closest('.message-row');
  if (old) old.remove();
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
        <button class="ghost del" data-del="${m.id}" title="${escapeAttr(tr('msg.delete'))}" style="padding:0 8px">×</button>
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
  if (!state.current) { toast(tr('settings.no_channel')); return; }
  const ch = state.channels.find((c) => c.id === state.current);
  if (!ch) return;
  $('setTitle').textContent = tr('settings.heading', { name: ch.name });
  $('setName').value = ch.name;
  $('setTopic').value = ch.topic || '';
  // 所属グループの選択肢を組み立てる(先頭は「グループなし」)。
  const sel = $('setGroup');
  const groups = state.groups.slice().sort((a, b) => a.position - b.position);
  sel.innerHTML = `<option value="">${escapeHtml(tr('group.none'))}</option>` +
    groups.map((g) => `<option value="${escapeAttr(g.id)}">${escapeHtml(g.name)}</option>`).join('');
  sel.value = ch.group_id || '';
  // チャンネル別通知設定。
  $('setNotify').checked = ch.notify;
  $('setSound').checked = ch.sound;
  $('setBadge').checked = ch.badge;
  await loadWebhooks();
  openModal('settingsModal');
}

async function saveChannel() {
  const res = await api('/api/channels/' + state.current, { method: 'PATCH', body: JSON.stringify({
    name: $('setName').value, topic: $('setTopic').value,
    group_id: $('setGroup').value,
    notify: $('setNotify').checked, sound: $('setSound').checked, badge: $('setBadge').checked,
  }) });
  if (!res.ok) { toast(tr('toast.save_failed')); return; }
  closeModals();
  await loadChannels(state.current);
  toast(tr('toast.saved'));
}

async function deleteChannel() {
  const ch = state.channels.find((c) => c.id === state.current);
  if (!confirm(tr('confirm.delete_channel', { name: ch ? ch.name : '' }))) return;
  const res = await api('/api/channels/' + state.current, { method: 'DELETE' });
  if (!res.ok) { const e = await res.json(); toast(e.error || tr('toast.delete_failed')); return; }
  closeModals();
  state.current = null;
  await loadChannels();
  toast(tr('toast.channel_deleted'));
}

async function loadWebhooks() {
  const list = await (await api(`/api/channels/${state.current}/webhooks`)).json();
  const el = $('webhookList');
  el.innerHTML = '';
  if (!list.length) { el.innerHTML = `<p class="muted">${escapeHtml(tr('webhook.none'))}</p>`; return; }
  for (const t of list) {
    const used = t.last_used_at ? tr('webhook.last_used', { time: fmtTime(t.last_used_at) }) : tr('webhook.unused');
    const div = document.createElement('div');
    div.className = 'wh';
    div.innerHTML = `
      <div class="row"><strong class="grow">${escapeHtml(t.name || tr('webhook.noname'))}</strong>
        <button class="danger" data-del="${t.id}">${escapeHtml(tr('common.delete'))}</button></div>
      <div class="url">${escapeHtml(t.url)}</div>
      <div class="row"><button class="secondary" data-copy>${escapeHtml(tr('common.copy'))}</button>
        <span class="muted">${escapeHtml(used)}</span></div>`;
    div.querySelector('[data-copy]').onclick = () => copyText(t.url);
    div.querySelector('[data-del]').onclick = async () => {
      if (!confirm(tr('confirm.delete_webhook'))) return;
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
  toast(tr('toast.webhook_created'));
}

async function clearMessages() {
  if (!confirm(tr('confirm.clear_history'))) return;
  await api(`/api/channels/${state.current}/messages/clear`, { method: 'POST' });
  closeModals();
  await loadMessages();
}

// ---- モーダル ----
function openModal(id) { $(id).classList.remove('hidden'); }
function closeModals() {
  $('addChannelModal').classList.add('hidden');
  $('settingsModal').classList.add('hidden');
  $('createdModal').classList.add('hidden');
  $('notifSheet').classList.add('hidden');
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
    // 編集・削除は通知/未読バッジを動かさず、開いている画面にだけ反映する(Discord と同じ)。
    if (ev.type === 'update') {
      if (ev.channel_id === state.current) onLiveUpdate(m);
      return;
    }
    if (ev.type === 'delete') {
      if (ev.channel_id === state.current) onLiveDelete(m && m.id);
      return;
    }
    if (ev.channel_id === state.current) {
      appendMessage(m);
    } else {
      state.unread[ev.channel_id] = (state.unread[ev.channel_id] || 0) + 1;
      renderChannelList();
    }
    // 新着フィードバック(音 + バイブ)。チャンネル別の通知音設定に従う。
    const ch = state.channels.find((c) => c.id === ev.channel_id);
    if (!ch || ch.sound) notifyFeedback();
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

// ---- 通知音・バイブ(画面を開いているときの新着フィードバック) ----
// 設計方針: 「気づける最小限・集中の邪魔にならない」。控えめな合成チャイムを
// Web Audio API でブラウザ内生成(音源ファイル不要=軽量)。OS 通知とは独立。
let audioCtx = null;

// ブラウザの自動再生ポリシー対策: 最初のユーザー操作で AudioContext を起こす。
function unlockAudio() {
  try {
    if (!audioCtx) {
      const AC = window.AudioContext || window.webkitAudioContext;
      if (AC) audioCtx = new AC();
    }
    if (audioCtx && audioCtx.state === 'suspended') audioCtx.resume();
  } catch (_) { /* 非対応環境は黙ってスキップ */ }
}

// playChime は2音(高→低)の控えめなベル風チャイムを鳴らす。
function playChime() {
  if (!audioCtx) return;
  try {
    const now = audioCtx.currentTime;
    // ソ→ミ(E5→…) 程度の優しい2音。音量は控えめ(0.12)。
    [[880, 0], [660, 0.12]].forEach(([freq, delay]) => {
      const osc = audioCtx.createOscillator();
      const gain = audioCtx.createGain();
      osc.type = 'sine';
      osc.frequency.value = freq;
      const t = now + delay;
      gain.gain.setValueAtTime(0.0001, t);
      gain.gain.exponentialRampToValueAtTime(0.12, t + 0.02);
      gain.gain.exponentialRampToValueAtTime(0.0001, t + 0.25);
      osc.connect(gain).connect(audioCtx.destination);
      osc.start(t);
      osc.stop(t + 0.3);
    });
  } catch (_) { /* 鳴らせなくても致命的でない */ }
}

// notifyFeedback は新着時のフィードバック(音 + バイブ)をまとめて出す。
function notifyFeedback() {
  if (!state.sound) return;
  playChime();
  // Android Chrome 等は短いバイブ。iOS Safari は Vibration API 非対応のため無反応。
  if (navigator.vibrate) {
    try { navigator.vibrate(80); } catch (_) {}
  }
}

function applySoundUI() {
  const sw = $('soundToggle');
  if (sw) sw.checked = state.sound;
}

function toggleSound() {
  state.sound = !state.sound;
  try { localStorage.setItem('sound', state.sound ? '1' : '0'); } catch (_) {}
  applySoundUI();
  if (state.sound) { unlockAudio(); playChime(); } // ON にした瞬間に確認音
}

// ---- 初期化 ----
async function init() {
  const me = await (await fetch('/api/me', { credentials: 'same-origin' })).json();
  applyI18n(me.lang);   // 静的 DOM を訳す。ログイン画面表示より前に確定させる。
  applySoundUI();       // wire() 時点では言語未確定なので、ここで訳語の title に直す。
  state.authRequired = me.auth_required;
  if (me.auth_required && !me.authed) { showLogin(); return; }
  showApp();
  if (me.auth_required) $('logoutBtn').classList.remove('hidden');

  await refreshNotifStatus();
  // URL の ?c= を初期選択に
  const c = new URL(location.href).searchParams.get('c');
  await loadChannels(c || null);
  connectSSE(); // 開いている画面へのリアルタイム配信を開始
  setInterval(tickRelativeTimestamps, 30000); // <t:..:R> の相対表記を更新し続ける
}

function wire() {
  $('loginBtn').onclick = login;
  $('password').addEventListener('keydown', (e) => { if (e.key === 'Enter') login(); });
  $('logoutBtn').onclick = logout;

  $('notifSettingsBtn').onclick = () => { refreshNotifStatus(); openModal('notifSheet'); };
  $('enableBtn').onclick = enableNotifications;
  $('disableBtn').onclick = disableNotifications;
  $('testBtn').onclick = testPush;
  $('soundToggle').onchange = toggleSound;
  applySoundUI();

  // 自動再生ポリシー対策: 最初のユーザー操作で AudioContext を起こす(一度だけ)。
  const unlockOnce = () => { unlockAudio(); document.removeEventListener('pointerdown', unlockOnce); document.removeEventListener('keydown', unlockOnce); };
  document.addEventListener('pointerdown', unlockOnce);
  document.addEventListener('keydown', unlockOnce);

  $('addChannelBtn').onclick = () => openModal('addChannelModal');
  $('addGroupBtn').onclick = addGroup;
  $('createChannelBtn').onclick = createChannel;
  $('newChannelName').addEventListener('keydown', (e) => { if (e.key === 'Enter') createChannel(); });

  $('settingsBtn').onclick = openSettings;
  $('saveChannelBtn').onclick = saveChannel;
  $('deleteChannelBtn').onclick = deleteChannel;
  $('createWebhookBtn').onclick = createWebhook;
  $('newWebhookName').addEventListener('keydown', (e) => { if (e.key === 'Enter') createWebhook(); });
  $('clearMessagesBtn').onclick = clearMessages;

  $('menuBtn').onclick = () => $('app').classList.toggle('drawer-open');
  // 背面スクリム(右側の余白)タップ: チャンネルは切り替えず閉じるだけ
  $('drawerScrim').onclick = () => $('app').classList.remove('drawer-open');

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
