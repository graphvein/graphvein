import { ApiHideProperty, ApiProperty } from "@nestjs/swagger";
import { Exclude } from "class-transformer";
import { BaseEntity, Column, Entity, OneToMany, PrimaryColumn } from "typeorm";
import type { Post } from "./Post";

export abstract class Stamped extends BaseEntity {
  @Column({ name: "created_by" })
  abstract createdBy: string;
}

@Entity({ name: "authors", schema: "blog", synchronize: false })
export class Author extends Stamped {
  @ApiProperty({ description: "the key" })
  @PrimaryColumn("varchar", { length: 36 })
  id: string;

  @ApiHideProperty()
  @Exclude()
  @Column({ select: false })
  secret: string;

  createdBy = "system";

  @OneToMany("Post", (post: Post) => post.author)
  posts: Post[];
}
