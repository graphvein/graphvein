mixin Walker on Object {                       // DA-T2
  void walk() {}
}
base mixin Swimmer {}                          // DA-T2
mixin class Runner {}                          // DA-T1 mixin: true
class Robot = Object with Walker;              // DA-T1 (mixin application)
sealed class Result {}                         // DA-T1

extension Doubling on int {                    // DA-T3
  int get doubled => this * 2;
}
extension on List<int> {                       // DA-T3 extension on List<int>
  int get total => fold(0, (a, b) => a + b);
}
extension on List<int> {                       // DA-T3 extension on List<int>@2 (BE-5)
  int get firstOrZero => isEmpty ? 0 : first;
}
extension<T> on Map<String, T>? {              // DA-T3 extension on Map<String,T>?
  bool get missing => this == null;
}
extension on void Function() {                 // DA-T3 extension on void Function()
  void twice() {}
}

extension type Meters(int value) {             // DA-T4
  Meters.zero() : value = 0;                   // DA-C1 Meters.zero
  int get kilometres => value ~/ 1000;
}

enum Color {                                   // DA-T5
  red,
  green;

  const Color();                               // DA-C1 Color.new
  String describe() => name;
}

typedef Callback = void Function();            // DA-R1

@pragma('vm:prefer-inline')
int get answer => 42;                          // DA-F3 (top-level function)
set answer(int value) {}                       // DA-F4 answer=

@JS()
external void alert(String message);           // DA-F1 (external and annotation in the span)

void main() {                                  // DA-F1
  int helper() => 1;                           // DA-F6 main.helper
  helper();
}

var handler = () {                             // DA-R1 (a variable)
  void inTopLambda() {}                        // DA-F6 inTopLambda (no owner)
};
