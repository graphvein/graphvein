import 'kinds.dart'; // DA-I2 (relative to lib/src)

/// Doc comments are outside every span.
@immutable
abstract class Shape {
  final int sides;

  Shape() : sides = 0;                       // DA-C1 Shape.new
  Shape.named(this.sides) : assert(sides >= 0); // DA-C1 Shape.named (the ; is in the span)
  const Shape.unit() : sides = 1;            // DA-C1 (const)
  factory Shape.square() => Square();        // DA-C1 (factory)
  factory Shape.circle() = Circle;           // DA-C1 (redirecting factory)

  @override
  // a comment between annotations stays in the span
  @Deprecated('use describe')
  String toString() => 'Shape($sides)';      // DA-F2, DA-S

  int get area => 0;                         // DA-F3 Shape.area
  set area(int value) {}                     // DA-F4 Shape.area=
  static Shape origin() => Square();         // DA-F2 (static)
  void draw();                               // DA-F2 (abstract; the ; is in the span)

  Shape operator +(Shape other) => this;     // DA-F5 operator +
  Shape operator -(Shape other) => this;     // DA-F5 operator -
  Shape operator -() => this;                // DA-F5 operator -@2 (BE-5)
  bool operator ==(Object other) => identical(this, other);
  int operator [](int index) => index;
  void operator []=(int index, int value) {}

  void paint() {
    void stroke() {                          // DA-F6 Shape.paint.stroke
      void dot() {}                          // DA-F6 Shape.paint.stroke.dot
    }
    final fill = () {
      void inLambda() {}                     // DA-F6 Shape.paint.inLambda (a lambda adds no owner)
    };
  }

  final callback = () {
    void inField() {}                        // DA-F6 Shape.inField (a field initializer: the type owns it)
  };
}

class Square extends Shape {
  Square() : super.named(4);
  @override
  void draw() {}
}

class Circle extends Shape {
  Circle() : super.named(0);
  @override
  void draw() {}
}
