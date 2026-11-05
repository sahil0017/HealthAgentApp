-- "Cholesterol total" is not LDL, and the sedimentation rate is not a red cell count.
UPDATE biomarker_catalog SET synonyms = array_remove(synonyms, 'Cholesterol total') WHERE code = 'CHOLESTEROL_IN_LDL';
UPDATE biomarker_catalog SET synonyms = array_remove(synonyms, 'Erythrocyte Sedimentation Rate') WHERE code = 'ERYTHROCYTE';
