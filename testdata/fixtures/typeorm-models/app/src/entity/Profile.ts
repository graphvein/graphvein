import { Column, Entity, PrimaryColumn } from "typeorm";

@Entity("profiles")
export class Profile {
  @PrimaryColumn()
  handle: string;

  @PrimaryColumn("int")
  region: number;

  @Column({ type: "smallint", default: -1 })
  rank: number;

  @Column({ type: "bigint", nullable: true })
  points: string;

  @Column({ type: "real" })
  score: number;

  @Column("boolean", { default: false })
  hidden: boolean;

  @Column({ type: "numeric", precision: 5 })
  pct: string;

  @Column({ type: "character", length: 3 })
  country: string;
}
