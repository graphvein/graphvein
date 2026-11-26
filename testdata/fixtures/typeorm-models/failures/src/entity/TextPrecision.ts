import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (14): a precision on text (Revision 4 A5): postgres rejects
// text(5) (type modifier is not allowed).
@Entity()
export class TextPrecision {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ type: "text", precision: 5 })
  body: string;
}
