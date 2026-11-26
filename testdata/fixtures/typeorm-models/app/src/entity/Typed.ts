import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";
import { Level } from "../constants/enums";

@Entity()
export class Typed {
  @PrimaryGeneratedColumn("identity")
  id: number;

  @Column({ type: "enum", enum: Level, default: Level.Basic })
  level: Level;

  @Column({ type: "enum", enum: ["a", "b"], enumName: "ab_kind", default: "a" })
  kind: string;

  @Column("text", { array: true })
  labels: string[];

  @Column("jsonb", { nullable: true })
  meta: object;

  @Column("timestamptz", { default: () => "now()" })
  at: Date;

  @Column("timestamp", { default: () => "CURRENT_TIMESTAMP(3)" })
  precise: Date;

  @Column("uuid")
  ref: string;

  @Column({ type: "varchar", length: 50, default: "its" })
  quoted: string;

  // Revision 4 A2: a number and a boolean string on a boolean column, as
  // postgres stores them (true).
  @Column({ default: 1 })
  flagNum: boolean;

  @Column("boolean", { default: "yes" })
  flagStr: boolean;

  // Revision 4 B1: a common function default the oracle compares.
  @Column("uuid", { default: () => "gen_random_uuid()" })
  token: string;

  // Revision 4 A1: string defaults on a timestamp are stored in the
  // database's own rendering (not claimed); "now()" is frozen at CREATE
  // TABLE.
  @Column("timestamp", { default: "2020-01-01" })
  since: Date;

  @Column("timestamp", { default: "now()" })
  frozen: Date;
}
