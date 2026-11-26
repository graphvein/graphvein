import { Column, Entity, PrimaryColumn } from "typeorm";

// TO-E1 synchronize: false: mapped, under the shown assumption.
@Entity({ name: "archive", synchronize: false })
export class Archive {
  @PrimaryColumn()
  id: number;

  @Column()
  label: string;
}
