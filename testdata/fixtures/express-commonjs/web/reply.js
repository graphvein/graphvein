// Every handler answers with its own name, so the oracle can tell which
// route served a request.
exports.reply = (name) => (req, res) => res.json({ route: name });
