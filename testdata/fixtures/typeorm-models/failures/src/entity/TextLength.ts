import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (13): a length on text on postgres (Revision 4 A5): postgres's
// driver gives text no length, so the metadata validator rejects it.
@Entity()
export class TextLength {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ type: "text", length: 200 })
  body: string;
}
