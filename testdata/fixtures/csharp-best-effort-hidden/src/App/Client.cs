using System;                                        // CS-I2 (no refused file names System)
using Gen.Messages;                                  // BE-8 (nested blocks Gen, Messages)
using Gen;                                           // BE-8 (equal to Gen)
using Wire;                                          // BE-8 (dotted prefix of Wire.Format)
using GenX.Tools;                                    // CS-I2 (not under Gen: whole segments)
using static Helpers.Text;                           // BE-8 (static: may name a global type; both refused files)
using Json = Newtonsoft.Json;                        // BE-8 (alias: may name a global type; both refused files)
using App.Models;                                    // CS-I1 (declared namespace)

namespace App;

public class Client
{
    public void Send() { }
}
