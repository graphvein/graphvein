import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (12): the string 'CURRENT_TIMESTAMP' on a MySQL datetime column
// (Revision 4 A1): MySQL rejects it as a default (mapped).
@Entity()
export class MyDatetime {
  @PrimaryGeneratedColumn()
  id: number;

  @Column("datetime", { default: "CURRENT_TIMESTAMP" })
  at: Date;
}
