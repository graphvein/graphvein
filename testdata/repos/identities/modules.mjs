export function run() {
  it("one", () => { const helper = () => 1; helper(); });
  it("two", () => { const helper = () => 2; helper(); });
}
const moduleObject = { save() { return 1; } };
