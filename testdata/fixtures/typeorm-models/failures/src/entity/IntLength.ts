import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (13): a length on int (Revision 4 A5): the metadata validator
// rejects a length on a type the driver gives none.
@Entity()
export class IntLength {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ type: "int", length: 11 })
  count: number;
}
