@Deprecated('fixture')
library acme.app;                                     // DA-I7

import 'dart:async';                                  // DA-I1
import 'src/shapes.dart';                             // DA-I2
import 'app.dart';                                    // DA-I2 self-import: no edge
import 'package:acme_app/src/kinds.dart' as kinds;    // DA-I3
import 'package:acme_core/core.dart';                 // DA-I4
import 'package:http/http.dart' show get;             // DA-I5
import 'src/io_stub.dart'                             // DA-I6: each URI on its own
    if (dart.library.io) 'src/io_native.dart'         //   DA-I2
    if (dart.library.js_interop) 'package:web/web.dart'; // DA-I5
import 'https://example.com/remote.dart';             // DA-I8 (another scheme)
import 'src/' 'shapes.dart';                          // DA-I8 (not one plain string literal)
import r'src/shapes.dart' deferred as lazy;           // DA-I2 (raw string)
import 'src/missing.dart';                            // DA-I2 missing: unresolved_import
import 'package:acme_app/missing.dart';               // DA-I3 missing: unresolved_import
import '../../outside.dart';                          // DA-I2 above the root: unresolved_import
export 'src/kinds.dart';                              // DA-I2 (export)
export 'package:acme_app/src/modern.dart';            // DA-I3 (export)
export 'dart:math';                                   // DA-I1 (export)

part 'src/app_part.dart';                             // DA-I2 (part: imports edge, O12)
part 'src/messages.g.dart';                           // DA-I2 (generated part, indexed: O9)

Future<void> run() async {
  await Future<void>.value();
}
