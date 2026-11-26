// An enum module: no ORM import, so it passes only gate row G12 and its
// payload holds only the enum declarations.
export enum PostStatus {
  Draft = 1,
  Review,
  Published = 5,
  Removed = -1,
}

export const enum Shade {
  Light = "light",
  "Dark Mode" = "dark",
}

enum Mixed {
  A = PostStatus.Draft,
  B = 1 << 2,
  C,
}

declare enum Ambient {
  X,
}

export const DEFAULT_SHADE = Shade.Light;
export const labels = ["a", "b"];
export default Mixed;
