import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (14): a precision on int (Revision 4 A5): postgres rejects
// integer(5).
@Entity()
export class IntPrecision {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ type: "int", precision: 5 })
  count: number;
}
