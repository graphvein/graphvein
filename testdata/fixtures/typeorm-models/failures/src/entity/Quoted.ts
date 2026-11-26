import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (4) an unescaped quote.
@Entity()
export class Quoted {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ default: "it's" })
  name: string;
}
