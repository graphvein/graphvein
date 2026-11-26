import {
  AfterLoad,
  Check,
  Column,
  CreateDateColumn,
  Entity,
  Index,
  JoinColumn,
  ManyToOne,
  OneToOne,
  PrimaryGeneratedColumn,
  Relation,
  Unique,
} from "typeorm";
import type * as geo from "./geo";
import { PostStatus, Shade } from "../constants/status";
import { Author } from "./Author";

@Entity("post")
@Index(["title", "status"], { unique: true, where: "title IS NOT NULL" })
@Unique("UQ_post_slug", ["slug"])
@Check(`"rating" >= 0`)
class Post {
  @PrimaryGeneratedColumn("uuid")
  id!: string;

  @Column({ type: "varchar", length: 120, nullable: true })
  title?: string | null;

  @Column()
  slug!: string;

  @Column({ type: "int", default: PostStatus.Draft })
  status!: PostStatus;

  @Column({ type: "simple-enum", enum: Shade, default: Shade.Light })
  shade!: Shade;

  @Column("decimal", { precision: 6, scale: 2, default: -1.5 })
  rating!: number;

  @Column({ default: () => "CURRENT_TIMESTAMP" })
  readonly publishedAt: Date = new Date();

  @Column("simple-array")
  tags: Array<string> = [];

  @Column({ type: "simple-json", nullable: true })
  meta: { a: number } & Extra;

  @Column() kind: "short" | "long" | 3 | -1 | true;

  declare draft: [string, number];

  location?: geo.Point;

  @ManyToOne(() => Author, (author) => author.posts, { onDelete: "CASCADE", eager: true })
  @Index()
  author!: Relation<Author>;

  @OneToOne(() => Author)
  @JoinColumn({ name: "editor_id", referencedColumnName: "id" })
  editor: Promise<Author | null>;

  @CreateDateColumn()
  created!: (Date);

  onSave: (post: Post) => void;

  scores: number[][];

  ["computed"]: number;

  [key: string]: unknown;

  static table = "post";

  constructor(title?: string) {
    this.title = title;
  }

  @AfterLoad()
  normalize(): void {}

  get label(): string {
    return `${this.title}`;
  }

  set label(value: string) {}
}

export default Post;
