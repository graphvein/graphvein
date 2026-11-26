// A barrel module that re-exports TypeORM's decorators. A re-export is not
// an import, so the file passes no gate row (GD); entity files importing
// from it pass gate row G11.
export { Column, Entity, PrimaryGeneratedColumn } from "typeorm";
