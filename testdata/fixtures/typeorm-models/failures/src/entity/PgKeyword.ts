import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (10): a string default naming a datetime keyword on a postgres
// timestamp column (Revision 4 A1): postgres rejects '<keyword>'.
@Entity()
export class PgKeyword {
  @PrimaryGeneratedColumn()
  id: number;

  @Column("timestamp", { default: "CURRENT_TIMESTAMP" })
  at: Date;
}
