#!/usr/bin/env node
'use strict';

const { run } = require('../index');

run(process.argv.slice(2))
  .then((exitCode) => {
    process.exit(exitCode);
  })
  .catch((err) => {
    console.error(`pigasus error: ${err.message}`);
    process.exit(1);
  });
