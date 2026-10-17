// Thin wrapper around the editor's JSON API.

async function req(method, url, body) {
  const opts = { method, headers: {} };
  if (body instanceof FormData) {
    opts.body = body;
  } else if (body !== undefined) {
    opts.headers['Content-Type'] = 'application/json';
    opts.body = JSON.stringify(body);
  }
  let res;
  try {
    res = await fetch(url, opts);
  } catch {
    throw new Error("Can't reach Sitesmith. Is it still running?");
  }
  const data = await res.json().catch(() => ({}));
  if (!res.ok) {
    const err = new Error(data.error || `Something went wrong (${res.status}).`);
    err.status = res.status;
    err.data = data;
    throw err;
  }
  return data;
}

export const api = {
  schema: () => req('GET', '/api/schema'),

  sites: () => req('GET', '/api/sites'),
  createSite: () => req('POST', '/api/sites'),
  openSite: (id) => req('POST', `/api/sites/${id}/open`),
  removeSite: (id) => req('DELETE', `/api/sites/${id}`),

  site: () => req('GET', '/api/site'),
  saveSite: (site) => req('PUT', '/api/site', site),
  setup: (opts) => req('POST', '/api/setup', opts),

  pages: () => req('GET', '/api/pages'),
  page: (id) => req('GET', `/api/pages/${id}`),
  createPage: (parentId, title) => req('POST', '/api/pages', { parentId, title }),
  updatePage: (id, fields) => req('PUT', `/api/pages/${id}`, fields),
  deletePage: (id) => req('DELETE', `/api/pages/${id}`),
  movePage: (id, parentId, index) => req('POST', `/api/pages/${id}/move`, { parentId, index }),
  duplicatePage: (id) => req('POST', `/api/pages/${id}/duplicate`),
  discardPage: (id, save) => req('POST', `/api/pages/${id}/discard`, { save }),

  shared: () => req('GET', '/api/shared'),
  createShared: (name, block) => req('POST', '/api/shared', { name, block }),
  updateShared: (id, fields) => req('PUT', `/api/shared/${id}`, fields),
  deleteShared: (id) => req('DELETE', `/api/shared/${id}`),

  versions: () => req('GET', '/api/versions'),
  saveVersion: (label) => req('POST', '/api/versions', { label }),
  restoreVersion: (id, save) => req('POST', `/api/versions/${id}/restore`, { save }),
  discardChanges: (save) => req('POST', '/api/discard', { save }),

  media: () => req('GET', '/api/media'),
  upload: (files) => {
    const fd = new FormData();
    for (const f of files) fd.append('files', f);
    return req('POST', '/api/media', fd);
  },
  deleteMedia: (id, force) => req('DELETE', `/api/media/${id}${force ? '?force=1' : ''}`),

  publish: () => req('POST', '/api/publish'),
  openFolder: () => req('POST', '/api/open-folder'),
};
