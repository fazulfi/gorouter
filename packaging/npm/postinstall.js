'use strict';
const launcher = require('./launcher');
if (!process.env.GOROUTER_SKIP_POSTINSTALL) launcher.install({
  version: process.env.GOROUTER_VERSION || 'v1.2.3',
  expectedSha256: process.env.GOROUTER_SHA256
}).catch((error) => { console.error(error.message); process.exitCode = 1; });
