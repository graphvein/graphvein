import { Column, Entity } from "typeorm";

// TO-P1 (1) no primary column.
@Entity()
export class NoKey {
  @Column()
  name: string;
}
