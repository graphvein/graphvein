function helper() {}

// TSX-15: a with statement's object may hold the name.
function legacy(o) {
  with (o) {
    helper(); // no edge
  }
  helper(); // edge
}
module.exports = { legacy };
