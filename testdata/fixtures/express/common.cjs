const express = require('express');
const app = express();
const router = express.Router();
function handler(req, res) { res.end(); }
app.use(router);
router.options('/cjs', handler);
router.delete('/cjs', handler);
router.patch('/cjs', handler);
router.get('/inline', function(req, res) { res.end(); });
