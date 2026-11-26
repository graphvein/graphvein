import { Column, Entity, Index, ManyToMany, PrimaryGeneratedColumn } from "typeorm";
import { Book } from "./Book";

@Entity()
export class Reader {
  @PrimaryGeneratedColumn()
  id: number;

  @Index()
  @Column()
  name: string;

  @ManyToMany(() => Book, (book) => book.readers)
  books: Book[];
}
