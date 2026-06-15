// scripts/test_gitlab_call.js
const path = require('path');

// Minimal host stub for testing the plugin locally
global.host = {
  process: {
    env: (k) => process.env[k]
  },
  config: {
    options: {
      gitlabApiKey: 'FAKE_API_KEY',
      gitlabBaseUrl: 'https://gitlab.crumpton.org'
    }
  },
  server: {
    logger: (lvl, msg) => console.log('[host.server.logger]', lvl, msg)
  },
  console: console,
  utils: {
    btoa: (s) => Buffer.from(s).toString('base64')
  }
};

// Implement host.http helpers used by plugin (both camelCase and dotted forms)
function makeResponder(name) {
  return function(url, headers, body) {
    console.log(`[host.http.${name}] url=`, url);
    console.log(`[host.http.${name}] headers=`, headers);
    if (body !== undefined) console.log(`[host.http.${name}] body=`, body);
    // return a simple successful JSON response
    return { status: 200, body: JSON.stringify([]) };
  };
}

host.http = {
  get: makeResponder('get'),
  post: makeResponder('post'),
  put: makeResponder('put')
};
// camelCase helpers
host.httpGet = host.http.get;
host.httpPost = host.http.post;
host.httpPut = host.http.put;

// Load plugin and call
const plugin = require('../plugins/gitlab.js');

const params = { CommandEvent: 'search_projects', page: 1, per_page: 20, search: 'mcp' };
console.log('Invoking plugin.call with params:', params);

const result = plugin.call(params);
console.log('Plugin returned:', result);
