extern alias Legacy;                                          // D (extern alias)
global using global::System.Linq;                             // CS-I2 (global, global::)
using System;                                                 // CS-I2 (plain)
using static System.Math;                                     // CS-I2 (static)
using Json = System.Text.Json.JsonSerializer;                 // CS-I2 (alias)
using Shop.Billing;                                           // CS-I1 (declared namespace)
using Shop;                                                   // CS-I1 (prefix of a declared namespace)
using static Shop.Billing.Invoice.Totals;                     // CS-I1 (static: nested type in a declared namespace)
using Inv = Shop.Billing.Invoice;                             // CS-I1 (alias: type in a declared namespace)
using Point = (int X, int Y);                                 // D (alias of a tuple type)

namespace Shop.Orders;

using Shipping;                                               // CS-I3 (inside a namespace: may be relative)

/// <summary>Excluded from the span.</summary>
[Serializable]
public partial class Order : IComparable<Order>, IEquatable<Order>, ILookup
{
    static Order() { }                                        // CS-F2 Order@static
    public Order() { }                                        // CS-F2 overloads (BE-6)
    public Order(int id, ref string name, params int[] lines) { }
    ~Order() { }                                              // CS-F3

    public int Id { get; set; }                               // CS-P1
    public string Name                                        // CS-P1 with a local function in an accessor (CS-F4)
    {
        get
        {
            string Trim(string s) => s.Trim();
            return Trim("x");
        }
    }

    int IComparable<Order>.CompareTo(Order other) => 0;       // CS-F1 explicit: CompareTo@IComparable
    public int CompareTo(object other) => 0;                  // CS-F1 ordinary member of the same name
    bool IEquatable<Order>.Equals(Order other) => true;
    public string this[int index] => "";                      // CS-P2 this@overload(int)
    public string this[string key, params object[] rest] => "";  // CS-P2 this@overload(string,params object[])
    string global::Shop.Billing.ILookup.this[int i] => "";    // CS-P2 explicit: this@ILookup
    int global::Shop.Billing.ILookup.Count => 0;              // CS-P1 explicit: Count@ILookup

    public static Order operator +(Order a, Order b) => a;    // CS-F5 binary +
    public static Order operator checked +(Order a, Order b) => a;
    public static Order operator -(Order a) => a;            // CS-F5 unary and binary - (BE-6)
    public static Order operator -(Order a, Order b) => a;
    public static Order operator >>(Order a, int n) => a;
    public static bool operator true(Order a) => true;
    public static bool operator false(Order a) => false;
    public static implicit operator int(Order o) => o.Id;     // CS-F5 conversions
    public static explicit operator Order(int id) => new Order();

    public void Run(out int a, in int b, scoped ref int c)    // CS-F1 with parameter modifiers
    {
        a = 0;
        int Local() => b;                                     // CS-F4 Order.Run.Local
        Func<int> f = () =>
        {
            int InLambda() => Local();                        // CS-F4: lambdas add no owner
            return InLambda();
        };
    }

    private readonly Func<int> field = () =>
    {
        int InField() => 1;                                   // CS-F4 in a field lambda: owned by the type (BE-4)
        return InField();
    };

    public event EventHandler Changed;                        // CS-R1
    public delegate void Handler(Order o);                    // CS-R1

    public struct Line { }                                    // CS-T2
    public readonly ref struct Span { }                       // CS-T2
    public record struct Money(decimal Amount);               // CS-T4 struct: true
    public record class Note(string Text);                    // CS-T4
    public enum Status { New, Paid }                          // CS-T5
    internal interface IAudit { void Audit(); }               // CS-T3 and its method

#if NET8_0
    public void Platform(int x) { }                           // CS-T6: every branch
#elif NET6_0
    public void Platform(int x) { }
#else
    public void Platform(int x) { }
#endif
}
