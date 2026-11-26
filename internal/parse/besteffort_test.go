package parse

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// bestEffortSamples are the T021 investigation sample files (one per language, every
// declaration form the claim contract names). The runtime rejects a module
// whose language ABI is outside 13-15 when it loads, so a clean parse here
// also shows that the registered module's ABI is accepted. The Dart sample
// declares its abstract method as `void abs();`: the investigation's `abstract void
// abs();` is not valid Dart and fails to parse with either Dart pin.
var bestEffortSamples = map[string]string{
	"java": `package com.example.app;

import java.util.List;
import java.util.*;
import static java.util.Objects.requireNonNull;
import static org.junit.Assert.*;

@Deprecated
public class Sample<T> extends Base implements Runnable {
    private int x;
    static { init(); }
    public Sample(int x) { this.x = x; }
    @Override
    public void run() { Runnable r = () -> {}; new Object() { void anon() {} }; }
    <U> U generic(U u) { return u; }
    static class Inner { void m() {} class Deeper {} }
    interface Callback { void call(); default void d() {} }
    enum Color { RED { void f() {} }, GREEN; Color() {} void m() {} }
    record Point(int x, int y) { Point { } Point(int x) { this(x, 0); } int sum() { return x + y; } }
    @interface Marker { String value() default ""; }
    void overloaded(int a) {}
    void overloaded(String a) {}
    void local() { class Local { void lm() {} } }
}
sealed interface Shape permits Circle {}
final class Circle implements Shape {}
`,
	"c_sharp": `using System;
using static System.Math;
using Json = System.Text.Json;
global using System.Linq;
using Foo.Bar;

namespace Contoso.App;

[Serializable]
public partial class Order<T> : BaseOrder, IOrder where T : class
{
    private int _x;
    public Order(int x) { _x = x; }
    static Order() { }
    ~Order() { }
    public int Total { get; set; }
    public int Computed => _x * 2;
    public string Name { get => n; init { n = value; } }
    public int this[int i] => i;
    public event EventHandler Changed { add { } remove { } }
    public static Order<T> operator +(Order<T> a, Order<T> b) => a;
    public static implicit operator int(Order<T> o) => o._x;
    public void Run() { int Local(int y) => y; void L2() { } }
    void IOrder.Explicit() { }
    public class Nested { public void M() { } }
    public delegate void Handler(int x);
}
public record Person(string First, string Last) { public string Full => First + Last; }
public record struct Coord(int X, int Y);
public readonly struct Money { public decimal Amount { get; } }
public interface IOrder { void Explicit(); int Default() => 1; }
public enum Status { Open, Closed }
public class Primary(int id) { public int Id => id; }
`,
	"dart": `library my.lib;

import 'dart:async';
import 'package:flutter/material.dart' as m show Widget hide Text;
import 'src/util.dart' deferred as util;
import 'stub.dart' if (dart.library.io) 'io.dart' if (dart.library.html) 'web.dart';
export 'src/api.dart' show Api;
part 'sample.g.dart';

typedef IntFn = int Function(int);

int topLevel(int a) => a + 1;
Future<void> asyncTop() async {}
int get topGetter => 1;
set topSetter(int v) {}
final int topVar = 1;

@immutable
abstract class Shape<T> extends Base with Mix implements Iface {
  Shape(this.x);
  Shape.named() : x = 0;
  factory Shape.make() => Circle();
  const Shape.c(this.x);
  final int x;
  int get area => 0;
  set area(int v) {}
  void draw() { void inner() {} }
  static int count() => 0;
  Shape operator +(Shape o) => this;
  external void ext();
  void abs();
}
sealed class Base {}
base mixin Mix on Base { void mixed() {} }
mixin class MC {}
extension StrX on String { int get len => length; void hi() {} }
extension on int { void anon() {} }
extension type Meters(int value) implements int { int twice() => value * 2; }
enum Color { red, green; const Color(); void paint() {} }
class Circle = Base with Mix;
`,
}

// brokenBestEffortSamples hold one unrecoverable declaration per language:
// row BE-2 of the T021 claim contract depends on these being reported as a
// syntax error rather than a clean tree.
var brokenBestEffortSamples = map[string]string{
	"java":    "class Broken { void m( { }\n",
	"c_sharp": "class Broken { void M( { }\n",
	"dart":    "class Broken { void m( { }\n",
}

// TestBestEffortGrammarsLoadAndParse pins the T021 grammar registration:
// each best-effort module is embedded, verified against its checksum,
// compiles, and parses its the investigation sample without an ERROR or MISSING node,
// while a broken file is flagged as a syntax error.
func TestBestEffortGrammarsLoadAndParse(t *testing.T) {
	var jobs []Job
	var want []bool
	for _, language := range []string{"java", "c_sharp", "dart"} {
		if _, ok := checksums[language]; !ok {
			t.Fatalf("%s is not registered", language)
		}
		jobs = append(jobs, Job{language, []byte(bestEffortSamples[language])}, Job{language, []byte(brokenBestEffortSamples[language])})
		want = append(want, false, true)
	}
	results, err := Batch(context.Background(), jobs, Options{Workers: 2, CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if r.Err != nil || r.Root == nil {
			t.Fatalf("%s job %d: %v", jobs[i].Language, i, r.Err)
		}
		if r.SyntaxError != want[i] {
			t.Errorf("%s job %d: syntax error %v, want %v", jobs[i].Language, i, r.SyntaxError, want[i])
		}
		if r.Root.Kind != "program" && r.Root.Kind != "compilation_unit" {
			t.Errorf("%s job %d: root kind %q", jobs[i].Language, i, r.Root.Kind)
		}
	}
}

// dartModernConstructors holds the Dart 3.12/3.13 primary and concise
// constructor forms next to the classic forms they must stay distinct from.
// Row DA-G1 guards `new name();` and row DA-G2 guards `factory name()`
// (T021), so the shapes below decide whether those guards stay.
const dartModernConstructors = `class Point(var int x, var int y);
class B(super.a, this.b) extends A { int b; void keep() {} }
class const P(final int x);
class P2.origin(final int x) {
  this : assert(x > 0);
}
class Empty;
enum E(final int v) { a(1), b(2); }
extension type const E2(int v);
class Classic {
  Classic(this.x);
  Classic.named() : x = 0;
  const Classic.c(this.x);
  factory Classic.make() => Classic(0);
  factory Classic.r() = Classic;
  factory Classic() => Classic(0);
  final int x;
}
class Concise {
  new(this.x);
  new named();
  const new c();
  const new();
  factory() => Concise(0);
  factory make() => Concise(0);
  final int x;
}
`

// dartShape summarises a declaration or signature node: its kind, the
// keyword tokens that tell concise constructors apart (`new`, `factory`),
// a `const` modifier, the `name` field and any bare identifier children.
// A detached node keeps only the last child of a repeated field, so in
// `Classic.named` the class name shows as id= and the constructor name as name=.
func dartShape(n *Node) string {
	shape := n.Kind
	for _, token := range n.Tokens {
		if token == "new" || token == "factory" {
			shape += " " + token
		}
	}
	for _, child := range n.Children {
		switch {
		case child.Kind == "const_builtin":
			shape += " const"
		case child == n.Fields["name"]:
			shape += " name=" + child.Text
		case child.Kind == "identifier":
			shape += " id=" + child.Text
		}
	}
	return shape
}

// dartShapes lists, in source order, each type with its primary constructor
// and the signature inside every class member.
func dartShapes(root *Node) []string {
	signatures := map[string]bool{
		"constructor_signature": true, "constant_constructor_signature": true,
		"factory_constructor_signature": true, "redirecting_factory_constructor_signature": true,
		"primary_constructor_body_signature": true, "function_signature": true,
	}
	var out []string
	var walk func(*Node)
	walk = func(n *Node) {
		switch n.Kind {
		case "class_definition", "enum_declaration", "extension_type_declaration":
			shape := dartShape(n)
			if primary := n.Fields["primary_constructor"]; primary != nil {
				shape += " primary(" + dartShape(primary) + ")"
			}
			out = append(out, shape)
		}
		if signatures[n.Kind] {
			out = append(out, "  "+dartShape(n))
			return
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(root)
	return out
}

// TestDartModernConstructorShapes records how the pinned tree-sitter-dart
// (0038280) parses Dart 3.12/3.13 constructors. Every form parses without an
// ERROR node. `new name();` is a constructor_signature with a `new` token
// (no longer DA-G1's function_signature returning `new`), so B4 tells it
// from `Classic()` by that token. `factory make()` keeps the one-identifier
// factory shape of `factory Classic()` (DA-G2): only a comparison with the
// enclosing type's name tells a named concise factory from the unnamed one.
func TestDartModernConstructorShapes(t *testing.T) {
	results, err := Batch(context.Background(), []Job{{"dart", []byte(dartModernConstructors)}}, Options{Workers: 1, CacheDir: filepath.Join(t.TempDir(), "cache")})
	if err != nil {
		t.Fatal(err)
	}
	r := results[0]
	if r.Err != nil || r.Root == nil {
		t.Fatalf("parse: %v", r.Err)
	}
	if r.SyntaxError {
		t.Fatal("modern constructors parse with a syntax error")
	}
	got := strings.Join(dartShapes(r.Root), "\n")
	want := strings.Join([]string{
		"class_definition name=Point primary(primary_constructor)",
		"class_definition name=B primary(primary_constructor)",
		"  function_signature name=keep",
		"class_definition const name=P primary(primary_constructor)",
		"class_definition name=P2 primary(primary_constructor name=origin)",
		"  primary_constructor_body_signature",
		"class_definition name=Empty",
		"enum_declaration name=E primary(primary_constructor)",
		"extension_type_declaration const name=E2",
		"class_definition name=Classic",
		"  constructor_signature name=Classic",
		"  constructor_signature id=Classic name=named",
		"  constant_constructor_signature const id=Classic name=c",
		"  factory_constructor_signature factory id=Classic id=make",
		"  redirecting_factory_constructor_signature factory id=Classic id=r",
		"  factory_constructor_signature factory id=Classic",
		"class_definition name=Concise",
		"  constructor_signature new",
		"  constructor_signature new name=named",
		"  constant_constructor_signature new const name=c",
		"  constant_constructor_signature new const",
		"  factory_constructor_signature factory",
		"  factory_constructor_signature factory id=make",
	}, "\n")
	if got != want {
		t.Errorf("shapes:\n%s\nwant:\n%s", got, want)
	}
}
