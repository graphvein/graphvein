using System.Text;                                            // CS-I2
using static Shop.Hidden.Message;                             // BE-8 (only the refused Broken.cs declares Shop.Hidden)
// Top-level statements: local functions have no owner (CS-F4).
var builder = new StringBuilder();
builder.Append(Add(1, 2));
Log();

int Add(int a, int b) => a + b;

static void Log()
{
    void Inner() { }                                          // CS-F4 Log.Inner
    Inner();
}

Action act = () =>
{
    void InTopLambda() { }                                    // CS-F4 without an owner
    InTopLambda();
};

class Helper                                                  // a type in the global namespace
{
    public static void Help() { }
}
