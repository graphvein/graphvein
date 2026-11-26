export interface User {
  id: string;
  name: string;
  email?: string | null;
  roles: Role[];
}

export type Role = 'admin' | 'member';

export interface NewComment {
  body: string;
  rating?: number;
}

export interface ArticleBody {
  article: {
    title: string;
    tags?: string[];
  };
}

export interface ArticleQuery {
  limit?: number;
  offset?: number;
  author?: string;
}

// `T | undefined` is optional, never nullable.
export interface Body13 {
  a: string | undefined;
  b?: string;
  c: string | null;
}
