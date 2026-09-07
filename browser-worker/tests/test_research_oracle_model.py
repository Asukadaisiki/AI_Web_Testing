import unittest

from app.models.research import ResearchOracleResult


class ResearchOracleModelTest(unittest.TestCase):
    def test_is_a_single_reference_only_table(self) -> None:
        table = ResearchOracleResult.__table__

        self.assertEqual(
            table.primary_key.columns.keys(),
            ["research_run_id"],
        )
        self.assertEqual(
            set(table.columns.keys()),
            {
                "research_run_id",
                "execution_id",
                "schema_version",
                "evaluator",
                "passed",
                "reason_code",
                "decision_json",
                "content_sha256",
                "created_at",
                "updated_at",
            },
        )
        self.assertNotIn("report_json", table.columns)
        self.assertNotIn("transcript_json", table.columns)
        self.assertNotIn("dom_json", table.columns)
