import { MigrationInterface, QueryRunner } from "typeorm";

// A migration the data source names (TO-U1: found, not read).
export class Init1700000000000 implements MigrationInterface {
  async up(queryRunner: QueryRunner): Promise<void> {
    await queryRunner.query(`CREATE TABLE "users" ("id" SERIAL NOT NULL)`);
  }

  async down(queryRunner: QueryRunner): Promise<void> {
    await queryRunner.query(`DROP TABLE "users"`);
  }
}
