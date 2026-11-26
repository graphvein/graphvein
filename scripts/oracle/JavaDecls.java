// The javac declaration oracle of T021 (rows JV-T1 to JV-T6, JV-F1 to JV-F4,
// JV-R1; docs/lang/best-effort.md "Oracle"). Run by scripts/oracle/java_decls.sh,
// never by CI.
//
// Usage: java JavaDecls ROOT FILELIST
//
// FILELIST names one .java file per line, relative to ROOT. For each file
// javac's parser (JavacTask.parse() only: no attribution, no annotation
// processing, nothing of the file runs) lists every class, interface, enum,
// record, annotation type, method and constructor as
//
//   path <TAB> kind <TAB> owner path <TAB> startLine:startCol-endLine:endCol
//
// with 1-based lines and 0-based UTF-8 byte columns. Anonymous class bodies
// and enum constant bodies are skipped (row JV-R1); the implicit class of a
// compact source file is not listed and adds no owner (row JV-F4). A file
// javac reports errors for is also listed as "#error <TAB> path".
import com.sun.source.tree.ClassTree;
import com.sun.source.tree.CompilationUnitTree;
import com.sun.source.tree.LineMap;
import com.sun.source.tree.MethodTree;
import com.sun.source.tree.NewClassTree;
import com.sun.source.tree.Tree;
import com.sun.source.tree.VariableTree;
import com.sun.source.util.JavacTask;
import com.sun.source.util.SourcePositions;
import com.sun.source.util.TreeScanner;
import com.sun.source.util.Trees;
import java.io.PrintStream;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.nio.file.Paths;
import java.util.ArrayDeque;
import java.util.Deque;
import java.util.List;
import javax.tools.Diagnostic;
import javax.tools.JavaCompiler;
import javax.tools.JavaFileObject;
import javax.tools.StandardJavaFileManager;
import javax.tools.ToolProvider;

public class JavaDecls {
    public static void main(String[] args) throws Exception {
        Path root = Paths.get(args[0]);
        List<String> files = Files.readAllLines(Paths.get(args[1]), StandardCharsets.UTF_8);
        PrintStream out = new PrintStream(System.out, false, StandardCharsets.UTF_8);
        JavaCompiler javac = ToolProvider.getSystemJavaCompiler();
        StandardJavaFileManager fm = javac.getStandardFileManager(null, null, StandardCharsets.UTF_8);
        for (String rel : files) {
            if (rel.isEmpty()) {
                continue;
            }
            Path file = root.resolve(rel);
            String text = Files.readString(file, StandardCharsets.UTF_8).replace("\r\n", "\n").replace('\r', '\n');
            boolean[] failed = {false};
            JavacTask task = (JavacTask) javac.getTask(null, fm, d -> {
                if (d.getKind() == Diagnostic.Kind.ERROR) {
                    failed[0] = true;
                }
            }, List.of("-proc:none", "--enable-preview", "--release", "21"), null, List.of(new Source(file.toUri(), text)));
            Iterable<? extends CompilationUnitTree> units = task.parse();
            SourcePositions positions = Trees.instance(task).getSourcePositions();
            for (CompilationUnitTree unit : units) {
                LineMap lines = unit.getLineMap();
                new TreeScanner<Void, String>() {
                    final Deque<String> types = new ArrayDeque<>();

                    String at(long p) {
                        long line = lines.getLineNumber(p);
                        long start = lines.getStartPosition(line);
                        int column = text.substring((int) start, (int) p).getBytes(StandardCharsets.UTF_8).length;
                        return line + ":" + column;
                    }

                    void print(String kind, String owner, Tree t) {
                        long s = positions.getStartPosition(unit, t), e = positions.getEndPosition(unit, t);
                        out.println(rel + "\t" + kind + "\t" + owner + "\t" + at(s) + "-" + at(e));
                    }

                    @Override
                    public Void visitClass(ClassTree t, String owner) {
                        // The implicit class of a compact source file has no
                        // source range of its own.
                        boolean implicit = positions.getStartPosition(unit, t) < 0 || positions.getEndPosition(unit, t) < 0;
                        String path = owner;
                        if (!implicit) {
                            String kind = switch (t.getKind()) {
                                case INTERFACE -> "interface";
                                case ENUM -> "enum";
                                case RECORD -> "record";
                                case ANNOTATION_TYPE -> "annotation";
                                default -> "class";
                            };
                            path = owner.isEmpty() ? t.getSimpleName().toString() : owner + "." + t.getSimpleName();
                            print(kind, path, t);
                        }
                        types.push(t.getSimpleName().toString());
                        for (Tree member : t.getMembers()) {
                            if (t.getKind() == Tree.Kind.ENUM && member instanceof VariableTree v
                                    && v.getInitializer() instanceof NewClassTree constant && constant.getClassBody() != null) {
                                scan(constant.getArguments(), path);
                                continue;
                            }
                            scan(member, path);
                        }
                        types.pop();
                        return null;
                    }

                    @Override
                    public Void visitMethod(MethodTree t, String owner) {
                        boolean constructor = t.getName().contentEquals("<init>");
                        String name = constructor ? types.peek() : t.getName().toString();
                        String path = owner.isEmpty() ? name : owner + "." + name;
                        print(constructor ? "constructor" : "method", path, t);
                        return super.visitMethod(t, path);
                    }

                    @Override
                    public Void visitNewClass(NewClassTree t, String owner) {
                        scan(t.getEnclosingExpression(), owner);
                        scan(t.getArguments(), owner);
                        return null;
                    }
                }.scan(unit, "");
            }
            if (failed[0]) {
                out.println("#error\t" + rel);
            }
        }
        out.flush();
    }

    /** Source feeds javac the LF-normalized text the contract's spans use. */
    static final class Source extends javax.tools.SimpleJavaFileObject {
        final String text;

        Source(java.net.URI uri, String text) {
            super(uri, JavaFileObject.Kind.SOURCE);
            this.text = text;
        }

        @Override
        public CharSequence getCharContent(boolean ignoreEncodingErrors) {
            return text;
        }
    }
}
