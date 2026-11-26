using System.Collections.Generic;                             // CS-I2

namespace Shop.Orders
{
    using Billing;                                            // CS-I3 (relative: Shop.Billing)

    // The second part of the partial class (CS-T1 partial: true).
    public partial class Order
    {
        public List<string> Lines() => new();
    }

    namespace Archive                                         // nested block namespace: Shop.Orders.Archive
    {
        internal sealed class Archived { }
    }
}
