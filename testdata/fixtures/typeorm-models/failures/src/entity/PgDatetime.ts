import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (2) datetime is not a postgres type: the postgres candidate fails, sqlite stays.
@Entity()
export class PgDatetime {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ type: "datetime" })
  at: Date;
}
