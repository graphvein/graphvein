import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (6) enum on sqlite.
@Entity()
export class SqliteEnum {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ type: "enum", enum: ["a", "b"] })
  kind: string;
}
