const express = require('express');

const legacy = express.Router();

// SH-E35: a callee declared twice is ambiguous, never followed.
legacy.post('/pick', (req, res) => {
  pick(req.body);
  res.end();
});

function pick(body) {
  return body.first;
}

function pick(body) {
  return body.second;
}

module.exports = { legacy };
