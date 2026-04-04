import { writable, derived } from 'svelte/store';

// { id, name, size, progress: 0-100, status: 'uploading'|'done'|'error', error: '' }
export const uploadTasks = writable([]);

export const activeTasks = derived(uploadTasks, $t => $t.filter(t => t.status === 'uploading'));
export const hasActiveTasks = derived(activeTasks, $t => $t.length > 0);

let nextId = 0;
const controllers = new Map();

export function addTask(name, size) {
  const id = ++nextId;
  const controller = new AbortController();
  controllers.set(id, controller);
  uploadTasks.update(t => [...t, { id, name, size, progress: 0, status: 'uploading', error: '' }]);
  return id;
}

export function getSignal(id) {
  return controllers.get(id)?.signal;
}

export function updateProgress(id, progress) {
  uploadTasks.update(t => t.map(x => x.id === id ? { ...x, progress } : x));
}

export function completeTask(id) {
  controllers.delete(id);
  uploadTasks.update(t => t.map(x => x.id === id ? { ...x, progress: 100, status: 'done' } : x));
}

export function failTask(id, error = '') {
  controllers.delete(id);
  uploadTasks.update(t => t.map(x => x.id === id ? { ...x, status: 'error', error } : x));
}

export function cancelTask(id) {
  const ctrl = controllers.get(id);
  if (ctrl) ctrl.abort();
  controllers.delete(id);
  uploadTasks.update(t => t.filter(x => x.id !== id));
}

export function removeTask(id) {
  controllers.delete(id);
  uploadTasks.update(t => t.filter(x => x.id !== id));
}

export function clearDone() {
  uploadTasks.update(t => t.filter(x => x.status === 'uploading'));
}
