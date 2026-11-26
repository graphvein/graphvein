import { Column, Entity, ManyToMany, OneToOne, PrimaryGeneratedColumn } from "typeorm";
import { Member } from "./Member";
import { Note } from "./Note";

// Inverse sides (TO-R3): no table fact.
@Entity()
export class Tag {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  label: string;

  @ManyToMany(() => Member, (member) => member.tags)
  members: Member[];

  @OneToOne(() => Note)
  note: Note;
}
