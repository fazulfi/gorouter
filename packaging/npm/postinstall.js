'use strict';
const launcher = require('./launcher');
if (!process.env.GOROUTER_SKIP_POSTINSTALL) launcher.install(launcher.resolveInstallOptions())
  .catch((error) => { console.error(error.message); process.exitCode = 1; });
