package com.acme.app;

import java.util.List;

/**
 * Javadoc stays outside the class span (JV-T1).
 */
@Deprecated
public class Sample<T> implements Runnable {
    private int count;                      // JV-R2 field
    static { init(); }                      // JV-R2 static initializer
    { count = 1; }                          // JV-R2 instance initializer

    public Sample(int count) { this.count = count; }        // JV-F2, BE-6
    public Sample(String @Size(max = 3) [] names, /* c */ int... more) { } // JV-F2, BE-6

    @Override
    public void run() {                                      // JV-F1
        Runnable r = () -> {
            class InLambda { void inside() {} }              // JV-T6 owned by run
        };
        Object o = new Object() {                            // JV-R1
            void anonymous() {}
            class Hidden {}
        };
    }

    <U> U generic(@Nullable U value, List<@Nullable ? extends U> list) { return value; } // JV-F1

    void overloaded(int a) {}                                // BE-6
    void overloaded(java.util.Map.Entry<String, int[]>[] entries) {}
    void overloaded(Sample<T> this, final String a) {}       // receiver dropped

    void blocks(boolean flag) {
        if (flag) {
            class Local { void lm() {} }                     // JV-T6
        } else {
            class Local { void lm() {} }                     // BE-5: Local@2
        }
    }

    static class Inner {                                     // JV-T6
        void m() {}
        class Deeper {}
    }

    interface Callback {                                     // JV-T2, JV-T6
        void call();
        default void call(int times) {}
    }

    enum Color {                                             // JV-T3
        RED { void special() {} },                           // JV-R1, JV-R2
        GREEN(2);
        private final int weight;
        Color() { this(1); }
        Color(int weight) { this.weight = weight; }
        int weight() { return weight; }
    }

    record Point(int x, int y) {                             // JV-T4
        Point { }                                            // JV-F2 compact
        Point(int x) { this(x, 0); }
        int sum() { return x + y; }
    }

    @interface Marker {                                      // JV-T5
        String value() default "";                           // JV-F3
        int[] weights();
    }

    private static void init() {}
}

sealed interface Shape permits Circle {}                     // JV-T2

final class Circle implements Shape {}                       // JV-T1
