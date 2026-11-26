global using global::System.Linq;                    // CS-I2 dotnet:System.Linq
using System;                                        // CS-I2 dotnet:System
using static System.Math;                            // CS-I2 dotnet:System.Math
using Json = System.Text.Json.JsonSerializer;        // CS-I2 dotnet:System.Text.Json.JsonSerializer
using App.Models;                                    // CS-I1 (declared namespace)
using Pair = (int Left, int Right);                  // D (alias of a tuple type)

namespace App.Services
{
    using Models;                                    // CS-I1 (relative: App.Models)
    using Logging;                                   // CS-I3 (may be relative: App.Services.Logging, App.Logging)

    public class Catalog
    {
        public Item First() => new("a");
    }
}
