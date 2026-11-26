import { Column, Entity, PrimaryGeneratedColumn, Tree, TreeChildren, TreeParent } from "typeorm";

export class Address {
  @Column()
  street: string;
}

// Row D: @Tree, its members and an embedded column.
@Entity()
@Tree("materialized-path")
export class Holder {
  @PrimaryGeneratedColumn()
  id: number;

  @Column()
  name: string;

  @TreeChildren()
  children: Holder[];

  @TreeParent()
  parent: Holder;

  @Column(() => Address)
  address: Address;
}
