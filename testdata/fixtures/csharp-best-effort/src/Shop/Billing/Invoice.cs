namespace Shop.Billing
{
    public interface ILookup
    {
        string this[int i] { get; }
        int Count { get; }
    }

    public static class InvoiceExtensions
    {
        public static void Touch(this Invoice invoice, [Obsolete] int /* c */ times) { }   // BE-6 types: (this Invoice,int)
        public static void Touch(this Invoice invoice) { }
    }

    public class Invoice
    {
        public class Totals
        {
            public static decimal Sum(decimal a, decimal b) => a + b;
        }
    }
}

namespace Shop.Results
{
    // C# generic arity: Result, Result`1 and Result`2 in one owner.
    public class Result { }
    public class Result<T> { public T Value { get; } }
    public record Result<T, E>(T Value, E Error);
}

namespace Shop.Other
{
    // Same name and arity as above in another namespace of the file:
    // namespaces are not owners, so BE-5 applies (Result`1@2).
    public class Result<T> { }
}
