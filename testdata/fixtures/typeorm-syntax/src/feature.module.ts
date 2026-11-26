// Only @nestjs/typeorm: no model payload (gate row GD); the forFeature
// call is in the ungated class-decorator cache (TemplateSyntax).
import { Module } from "@nestjs/common";
import { TypeOrmModule } from "@nestjs/typeorm";
import { Author } from "./entity/Author";

@Module({ imports: [TypeOrmModule.forFeature([Author])] })
export class FeatureModule {}
