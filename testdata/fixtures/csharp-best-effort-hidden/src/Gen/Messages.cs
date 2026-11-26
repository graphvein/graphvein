namespace Gen
{
    namespace Messages
    {
        // BE-2: `#if` inside a base list yields ERROR nodes. The text
        // names Gen and Messages (nested: Gen.Messages, under Gen) and
        // Wire.Format below, and the file may also declare types outside
        // them, in the global one.
        public sealed class Ping : IMessage
#if !COMPAT
            , IBufferMessage
#endif
        {
        }
    }
}

namespace Wire.Format
{
}
