import { Entity, PrimaryGeneratedColumn } from "typeorm";

@Entity()
export class Old {
  @PrimaryGeneratedColumn()
  id: number;
}
