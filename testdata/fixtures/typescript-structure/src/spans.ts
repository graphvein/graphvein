// Span conventions (O-S3-5): declare outside the span, body-less class
// members end before `;`, a type alias without `;` ends after a line
// comment on its last line.
declare function ambientSpan(x: number): void;
export declare abstract class AmbientClass { m(): void; }
export class Members {
  over(a: string): void;
  over(a: any): void {}
}
export type NoSemi = string // comment
export interface Repeated { a: 1 }
export interface Repeated { b: 2 }
