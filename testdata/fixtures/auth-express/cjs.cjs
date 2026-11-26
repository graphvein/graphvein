// CommonJS require of passport (row DA-E2) and an unmounted router.
const express = require('express');
const passport = require('passport');

const router = express.Router();
router.use(passport.authenticate('bearer', { session: false }));
router.get('/cjs', (req, res) => res.end());
module.exports = router;
