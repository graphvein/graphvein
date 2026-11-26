// The oracle's handler: answers with the route's name as written at the
// registration site, so a response says which route matched.
export function reply(label) {
  return function replied(req, res) {
    res.json({ route: "reply('" + label + "')" });
  };
}
