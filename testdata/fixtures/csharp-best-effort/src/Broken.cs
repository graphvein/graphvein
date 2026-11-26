using Shop.Hidden;

namespace Shop.Hidden;

// BE-2: `#if` inside a base list (eShop's committed gRPC code does this)
// yields ERROR nodes, so nothing in this file is claimed. Its text has one
// file-scoped declaration, so BE-8 hides only usings of Shop.Hidden, names
// under it and its prefix Shop; other usings stay dotnet: externals.
public sealed class Message : IMessage
#if !COMPAT
    , IBufferMessage
#endif
{
    public void Keep() { }
}
