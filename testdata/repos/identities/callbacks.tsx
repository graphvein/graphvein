function unique() { return 1; }
function tests() {
  it("first", () => {
    function helper() { return unique(); }
    helper();
  });
  it("second", () => {
    function helper() { return unique(); }
    helper();
  });
  jest.mock("one", () => {
    const mock = () => 3;
    mock();
  });
  jest.mock("two", () => {
    const mock = () => 4;
    mock();
  });
}
function hooks() {
  useMemo(() => {
    function visit() { return <span />; }
    visit();
  });
  useEffect(() => {
    function visit() { return <div />; }
    visit();
  });
}
