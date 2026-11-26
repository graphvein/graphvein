import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (11): a function text as a string default on a postgres uuid
// column (Revision 4 A1): postgres rejects it as a uuid.
@Entity()
export class PgUuidText {
  @PrimaryGeneratedColumn()
  id: number;

  @Column("uuid", { default: "uuid_generate_v4()" })
  token: string;
}
