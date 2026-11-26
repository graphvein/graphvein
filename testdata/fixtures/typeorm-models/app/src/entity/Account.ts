import { ApiProperty } from "@nestjs/swagger";
import { BeforeInsert, Column, DeleteDateColumn, Entity, VersionColumn } from "typeorm";
import { Level, Shade } from "../constants/enums";
import { Base } from "./Base";

@Entity()
export class Account extends Base {
  @Column({ length: 40 })
  code: string;

  @Column("varchar", { length: 12, unique: true })
  handle: string;

  @Column({ type: "decimal", precision: 10, scale: 2, default: 0 })
  balance: string;

  @Column({ default: true })
  active: boolean;

  @Column({ type: "double precision", default: 1.5 })
  ratio: number;

  @Column({ type: "text", nullable: true })
  bio: string | null;

  @Column({ name: "display_name", default: "anon" })
  displayName: string;

  @Column({ type: "int", default: Level.Pro })
  level: Level;

  @Column("simple-array")
  tags: string[];

  @Column("simple-json", { nullable: true })
  prefs: object;

  @Column({ type: "simple-enum", enum: Shade, default: Shade.Red })
  shade: Shade;

  @Column({ type: "date", nullable: true })
  birthday: string;

  @Column({ type: "time" })
  opensAt: string;

  @Column({ type: "text", default: () => "'none'" })
  motto: string;

  @Column({ select: false, comment: "not compared" })
  secret: string;

  @ApiProperty()
  @Column({ default: () => "CURRENT_TIMESTAMP" })
  seenAt: Date;

  @Column({ default: makeNick() })
  nick: string;

  @Column()
  count: number;

  @DeleteDateColumn()
  deletedAt: Date;

  @VersionColumn()
  version: number;

  @BeforeInsert()
  touch() {}
}

function makeNick(): string {
  return "nick";
}
