import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { Thing } from "./entity/thing.entity";
import { options } from "./options";

// TO-K7 `TypeOrmModule.forRoot(X)`; TO-K3 a literal forRoot with the 12.x
// data source name argument.
@Module({
  imports: [
    TypeOrmModule.forRoot(options),
    TypeOrmModule.forRoot({ type: "sqlite", database: "second.sqlite", entities: [Thing] }, "second"),
  ],
})
export class AppModule {}
