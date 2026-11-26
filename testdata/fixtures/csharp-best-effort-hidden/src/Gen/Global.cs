// BE-2: `#if` inside a base list yields ERROR nodes. No declaration of a
// named scope appears in this text, so every type here sits in the global
// one: it can hide a static or alias using, never a plain one.
public sealed class Pong : IMessage
#if !COMPAT
    , IBufferMessage
#endif
{
}
