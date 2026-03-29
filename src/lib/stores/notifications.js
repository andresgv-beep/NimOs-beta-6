import { writable, derived } from 'svelte/store';

export const notifications = writable([]);
export const unreadCount = derived(notifications, $n => $n.filter(x => !x.read).length);

let nextId = 0;

export function notify(message, options = {}) {
  const {
    type     = 'info',
    category = 'notification',
    title    = '',
    bubble   = true,
  } = options;

  const id = ++nextId;
  notifications.update(n => [{
    id, type, category, title, message,
    timestamp:  new Date().toISOString(),
    read:       false,
    showBubble: bubble,
  }, ...n]);
  return id;
}

export function notifySuccess(message, title = '')  { return notify(message, { type: 'success', title }); }
export function notifyError(message,   title = '')  { return notify(message, { type: 'error',   title }); }
export function notifyWarning(message, title = '')  { return notify(message, { type: 'warning',  title }); }
export function notifyInfo(message,    title = '')  { return notify(message, { type: 'info',     title }); }
export function notifySecurity(message, title = 'Seguridad') {
  return notify(message, { type: 'security', category: 'system', title });
}
export function notifySystem(message, title = 'Sistema') {
  return notify(message, { type: 'info', category: 'system', title });
}

export function markRead(id) {
  notifications.update(n => n.map(x => x.id === id ? { ...x, read: true } : x));
}
export function markAllRead() {
  notifications.update(n => n.map(x => ({ ...x, read: true })));
}
export function dismissNotification(id) {
  notifications.update(n => n.filter(x => x.id !== id));
}
export function clearCategory(category) {
  notifications.update(n => n.filter(x => x.category !== category));
}
export function clearAll() {
  notifications.set([]);
}
export function hideBubble(id) {
  notifications.update(n => n.map(x => x.id === id ? { ...x, showBubble: false } : x));
}
