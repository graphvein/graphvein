// Dart 3.12/3.13 constructors; tree-sitter-dart 0038280 parses every form
// below without an error.
class Point(var int x, var int y);              // DA-T1; primary constructor: DA-R1

class Named.origin(final int x) {               // DA-T1; named primary constructor: DA-R1
  this : assert(x >= 0);                        // DA-R1 (primary constructor body)
}

class Empty;                                    // DA-T1 (no body)

enum Level(final int value) { low(1), high(2) } // DA-T5; primary constructor: DA-R1

class Concise {
  final int x;
  new(this.x);                                  // DA-C2 Concise.new
  new named() : x = 0;                          // DA-C2 Concise.named (new token and name)
  const new zero() : x = 0;                     // DA-C2 Concise.zero (const)
  factory make() => Concise(2);                 // DA-C3 Concise.make (differs from the type name)
  factory red() = Concise.named;                // DA-C3 Concise.red (redirecting)
  factory Concise.legacy() => Concise(3);       // DA-C1 (two identifiers)
  untyped() {}                                  // DA-G1: an untyped method, read as a constructor
  later();                                      // DA-G1
}

class Constant {
  const new();                                  // DA-C2 Constant.new (const, no name)
}

class Made {
  Made._();                                     // DA-C1 Made._
  factory() => Made._();                        // DA-C3 Made.new (no identifier)
}

class Classic {
  factory Classic() => Classic._();             // DA-C1 Classic.new (one identifier, the type name)
  Classic._();
}

class Wrong {
  factory Other.make() => Wrong();              // DA-G2: not Dart (the first identifier is not the type)
}

class Torn {
  final int x;
  Torn.new() : x = 0;                           // DA-C1 Torn.new (`.new`: the unnamed constructor, Dart 2.15)
}

class TornConst {
  const TornConst.new();                        // DA-C1 TornConst.new (const)
}

class TornOther {
  Other.new();                                  // DA-G1: `.new` after a name that is not the type
}
