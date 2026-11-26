import { Column, Entity, PrimaryGeneratedColumn } from "typeorm";

// TO-P1 (5) a fractional default on an integer column.
@Entity()
export class Fraction {
  @PrimaryGeneratedColumn()
  id: number;

  @Column({ default: 1.5 })
  ratio: number;
}
