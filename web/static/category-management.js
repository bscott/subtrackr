function startEditCategory(id) {
    document.getElementById(`edit-category-form-${id}`).classList.remove('hidden');
    document.getElementById(`category-name-${id}`).classList.add('hidden');
    document.getElementById(`edit-btn-${id}`).classList.add('hidden');
}
function cancelEditCategory(id) {
    document.getElementById(`edit-category-form-${id}`).classList.add('hidden');
    document.getElementById(`category-name-${id}`).classList.remove('hidden');
    document.getElementById(`edit-btn-${id}`).classList.remove('hidden');
} 